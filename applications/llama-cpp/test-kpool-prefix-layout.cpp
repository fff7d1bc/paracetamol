// Independent integer-layout oracle. No model, GPU, native ABI or production scanner reuse.
#include <algorithm>
#include <array>
#include <cassert>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <limits>
#include <memory>
#include <random>
#include <set>
#include <stdexcept>
#include <string>
#include <vector>

using llama_pos = int32_t;
using llama_seq_id = int32_t;
constexpr int LLAMA_MAX_SEQ = 4;
constexpr llama_pos POS_CLEAN = std::numeric_limits<llama_pos>::max();
#define GGML_ASSERT(x) do { if (!(x)) throw std::runtime_error("production assertion"); } while (0)
#define LLAMA_LOG_ERROR(...) std::fprintf(stderr, __VA_ARGS__)
#define GGML_ABORT(...) throw std::runtime_error("production verifier mismatch")
using Pair = std::pair<llama_pos,uint32_t>;
using Positions = std::set<Pair>;
struct MockCells {
 std::array<Positions,LLAMA_MAX_SEQ> positions;
 const Positions & seq_pos_get(int s) const { return positions.at(s); }
};
struct MockCache {
 uint32_t streams = 1;
 std::array<MockCells,LLAMA_MAX_SEQ> cells;
 uint32_t get_n_stream() const { return streams; }
 const MockCells & get_cells(int s) const { return cells.at(s); }
 uint32_t get_stream(int s) const { return uint32_t(s); }
 Positions & seq(int s) { return cells.at(streams==1?0:s).positions.at(s); }
};
class llama_memory_hybrid_idx {
public:
 struct kpool_layout;
 const kpool_layout & kpool_layout_update();
 std::unique_ptr<kpool_layout> kpool_lay;
 std::unique_ptr<MockCache> mem_idx = std::make_unique<MockCache>();
 std::array<llama_pos,LLAMA_MAX_SEQ> mem_idx_stale{0,0,0,0};
 struct { bool indexer_kpool_by_order=false; } hparams_idx;
 bool kpool_verify=false;
 uint32_t pool=4;
 uint32_t get_kpool() const { return pool; }
};
#include "production-decl.inc"
#include "production-impl.inc"

struct Oracle {
 std::vector<Pair> cells;
 std::vector<uint32_t> starts;
 llama_pos min=0;
 size_t next=0;
};
static Oracle rebuild(const Positions & positions,uint32_t width,bool order) {
 Oracle out;
 out.cells={positions.begin(),positions.end()};
 if (out.cells.empty()) return out;
 out.min=out.cells.front().first;
 // Decide every possible block independently; unlike production this does not resume
 // a scanner, retain old state, or use its contiguous-position loop.
 size_t candidate=0;
 while (candidate+width<=out.cells.size()) {
  bool valid=order;
  if (!order) {
   std::set<llama_pos> actual;
   for (size_t x=candidate;x<candidate+width;++x) actual.insert(out.cells.at(x).first);
   const int64_t start=out.cells.at(candidate).first;
   valid=(start-out.min)%width==0 && actual.size()==width &&
         int64_t(*actual.rbegin())-int64_t(*actual.begin())+1==width;
  }
  if (valid) { out.starts.push_back(uint32_t(candidate)); candidate+=width; }
  else ++candidate;
 }
 out.next=candidate;
 return out;
}
static size_t updates=0;
static void check(llama_memory_hybrid_idx & m,const char * label) {
 const auto & layout=m.kpool_layout_update();
 uint32_t total=0;
 for(int s=0;s<LLAMA_MAX_SEQ;++s) {
  const auto & q=layout.seqs.at(s);
  const bool active=m.mem_idx->streams==1 || s<int(m.mem_idx->streams);
  Oracle ref=active?rebuild(m.mem_idx->seq(s),m.pool,m.hparams_idx.indexer_kpool_by_order):Oracle();
  const uint32_t stream=active&&m.mem_idx->streams!=1?uint32_t(s):0;
  if(q.cells!=ref.cells || q.pools!=ref.starts || q.pos_min!=ref.min || q.j_next!=ref.next || q.strm!=stream)
   throw std::runtime_error(std::string("integer oracle mismatch: ")+label+" seq="+std::to_string(s));
  for (uint32_t j:q.pools) {
   if(size_t(j)+m.pool>q.cells.size()) throw std::runtime_error("pool guard overrun");
   for(uint32_t k=0;k<m.pool;++k) if(q.cells.at(j+k).second>=1000000) throw std::runtime_error("cell bound");
  }
  total+=uint32_t(ref.starts.size());
 }
 if(total!=layout.n_pool_real) throw std::runtime_error("global pool count");
 m.mem_idx_stale.fill(POS_CLEAN);
 ++updates;
}
static void populate(Positions & p,int start,int end,uint32_t salt=0) {
 for(int pos=start;pos<end;++pos) p.emplace(pos,uint32_t(pos+1000)*13+salt);
}
static void erase_tail(Positions & p,int stale) { p.erase(p.lower_bound({stale,0}),p.end()); }
static void scenario(uint32_t width,bool order,uint32_t streams,bool verifier) {
 llama_memory_hybrid_idx m;
 m.pool=width; m.hparams_idx.indexer_kpool_by_order=order; m.mem_idx->streams=streams; m.kpool_verify=verifier;
 check(m,"empty-reservation");
 for(int s=0;s<(streams==1?LLAMA_MAX_SEQ:int(streams));++s) populate(m.mem_idx->seq(s),17,153,uint32_t(s));
 check(m,"initial-multisequence");
 check(m,"reservation-repeat-no-mutation");
 for(int drafts=0;drafts<=2;++drafts) for(int step=0;step<12;++step) {
  auto & p=m.mem_idx->seq(0);
  const int next=153+step;
  populate(p,next,next+1+drafts);
  check(m,"append-target-plus-drafts");
  erase_tail(p,next+1); m.mem_idx_stale[0]=next+1;
  check(m,"reject-drafts-0-1-2");
 }
 // Every cut residue and unchanged counts after rewriting the cell IDs.
 for(int cut=18;cut<70;++cut) {
  auto & p=m.mem_idx->seq(0);
  erase_tail(p,cut); populate(p,cut,175,900);
  m.mem_idx_stale[0]=cut; check(m,"same-count-edit-crossing-pool");
 }
 // Position gaps, duplicate M-RoPE image positions, order-based grouping.
 auto & p=m.mem_idx->seq(0);
 for(int pos=20;pos<80;++pos) {p.emplace(pos,70000+uint32_t(pos)); if(pos%3==0)p.emplace(pos,80000+uint32_t(pos));}
 for(auto it=p.begin();it!=p.end();) if(it->first>=90&&it->first<103)it=p.erase(it);else ++it;
 m.mem_idx_stale[0]=20; check(m,"image-duplicate-positions-and-holes");
 // Whole-sequence copy/move/restore and shifted first position must invalidate.
 auto saved=p;
 if(streams==1||streams>1) {
  m.mem_idx->seq(1)=p; m.mem_idx_stale[1]=0; check(m,"copy-destination-rebuild");
  p.clear(); m.mem_idx_stale[0]=0; check(m,"move-source-empty");
  p=saved; m.mem_idx_stale[0]=0; check(m,"restore-different-placement");
 }
 Positions shifted; for(auto x:p)shifted.emplace(x.first+200,x.second+400);
 p=shifted; m.mem_idx_stale[0]=0; check(m,"pos-min-change");
 // A missing earlier cell despite a clean append signal triggers the count fallback.
 p.erase(p.begin()); check(m,"count-mismatch-fallback");
 // Repeat a long deterministic tail-edit history with positions below and above the min.
 std::mt19937 rng(0x32137+width+streams);
 for(int n=0;n<450;++n) {
  int s=int(rng()%(streams==1?LLAMA_MAX_SEQ:streams)); auto & target=m.mem_idx->seq(s);
  if(n%41==0) {target.clear();m.mem_idx_stale[s]=0;}
  else if(n%19==0) { target=saved;m.mem_idx_stale[s]=0; }
  else {
   int min=target.empty()?17:target.begin()->first;
   int end=target.empty()?min:target.rbegin()->first+1;
   if(n%3==0&&end>min) {
    int cut=min+int(rng()%uint32_t(end-min+1));erase_tail(target,cut);
    m.mem_idx_stale[s]=cut<=min?0:cut;
    for(int k=0;k<int(rng()%12);++k)target.emplace(cut+k,100000+uint32_t(n*16+k));
   }else for(int k=0;k<int(rng()%9);++k)target.emplace(end+k,200000+uint32_t(n*16+k));
  }
  check(m,"deterministic-edit-sequence");
 }
 for(int s=0;s<LLAMA_MAX_SEQ;++s) {if(streams==1||s<int(streams))m.mem_idx->seq(s).clear();m.mem_idx_stale[s]=0;}
 check(m,"clear-all");
}
int main(int argc,char ** argv) {
 try {
  const bool verifier=argc==2&&std::string(argv[1])=="--verify";
  if(argc!=1&&!verifier)throw std::runtime_error("usage: test-kpool-prefix-layout [--verify]");
  for(uint32_t width:{1u,2u,3u,4u,7u,8u,16u,64u}) for(bool order:{false,true}) for(uint32_t streams:{1u,2u,4u}) scenario(width,order,streams,verifier);
  if(updates!=28032)throw std::runtime_error("integer layout coverage changed");
  std::printf("PASS independent integer layout updates=%zu verifier=%d\n",updates,int(verifier));
  return 0;
 }catch(const std::exception & e){std::fprintf(stderr,"FAIL %s\n",e.what());return 1;}
}
