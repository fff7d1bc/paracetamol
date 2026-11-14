// CPU metadata/oracle fixture: calls the actual matching-source production API.
// No inference, model weights, device discovery, or extracted implementation.
#include "llama-memory-hybrid-idx.h"
#include "llama-model.h"
#include "llama-batch.h"
#include "ggml-backend.h"
#include "ggml-cpu.h"
#include <algorithm>
#include <array>
#include <cmath>
#include <cstring>
#include <cstdio>
#include <map>
#include <memory>
#include <numeric>
#include <set>
#include <stdexcept>
#include <string>
#include <vector>
#include <sys/wait.h>
#include <unistd.h>

static void need(bool ok, const std::string & s) { if (!ok) throw std::runtime_error(s); }
static constexpr int K = 4, SIZE = 1024, SEQS = 4;
using cell = std::pair<llama_pos, uint32_t>;
struct pool { int seq, stream; std::vector<cell> members; int64_t rep() const { return stream*SIZE+members.back().second; } };

// Each tensor has 64-byte guards independent of its declared ggml shape.
struct tensor {
    unsigned char * bytes;
    size_t capacity;
    ggml_context * ctx;
    ggml_backend_buffer_t buffer;
    ggml_tensor * t;
    size_t size;
    tensor(ggml_type type, int64_t a, int64_t b=1, int64_t c=1, int64_t d=1) {
        ctx = ggml_init({ggml_tensor_overhead()*2, nullptr, true});
        t = ggml_new_tensor_4d(ctx,type,a,b,c,d);
        size = ggml_nbytes(t);
        capacity = (size+128+63)/64*64;
        bytes = static_cast<unsigned char *>(std::aligned_alloc(64,capacity));
        need(bytes!=nullptr,"aligned tensor allocation");
        std::memset(bytes,0xa5,capacity);
        buffer = ggml_backend_cpu_buffer_from_ptr(bytes, capacity);
        t->buffer = buffer;
        t->data = bytes+64;
        std::memset(t->data, 0xcd, size);
    }
    ~tensor() { ggml_backend_buffer_free(buffer); ggml_free(ctx); std::free(bytes); }
    void guards() const {
        auto p = bytes;
        for (size_t i=0;i<64;++i) need(p[i]==0xa5,"leading tensor guard");
        for (size_t i=64+size;i<capacity;++i) need(p[i]==0xa5,"trailing tensor guard");
    }
    template<class T> T at(size_t i) const { return static_cast<T *>(t->data)[i]; }
};

static llama_ubatch batch(const std::vector<std::vector<int>> & sets, int first, int count, bool image=false) {
    llama_ubatch u{};
    u.b_equal_seqs=1; u.n_seq_tokens=count; u.n_seqs=sets.size();
    u.n_tokens=count*sets.size(); u.n_pos=image?4:1;
    u.data=std::make_shared<llama_ubatch::data_t>(); auto & d=*u.data;
    d.pos.resize(u.n_tokens*u.n_pos); d.token.assign(u.n_tokens,1);
    d.n_seq_id.resize(u.n_tokens); d.seq_id.resize(u.n_tokens);
    std::set<int> unique;
    for (auto & ss:sets) unique.insert(ss.begin(),ss.end());
    d.seq_id_unq.assign(unique.begin(),unique.end()); u.n_seqs_unq=unique.size();
    d.seq_id_data.reserve(u.n_tokens*SEQS);
    for (size_t s=0;s<sets.size();++s) for (int j=0;j<count;++j) {
        size_t i=s*count+j; d.n_seq_id[i]=sets[s].size();
        size_t start=d.seq_id_data.size();
        d.seq_id_data.insert(d.seq_id_data.end(),sets[s].begin(),sets[s].end());
        d.seq_id[i]=d.seq_id_data.data()+start;
        d.pos[i]=first+(image?j/3:j);
        if (image) { d.pos[u.n_tokens+i]=j%3+7; d.pos[2*u.n_tokens+i]=j/3+11; d.pos[3*u.n_tokens+i]=d.pos[i]; }
    }
    u.token=d.token.data(); u.pos=d.pos.data(); u.n_seq_id=d.n_seq_id.data();
    u.seq_id=d.seq_id.data(); u.seq_id_unq=d.seq_id_unq.data();
    return u;
}

struct fixture {
    bool unified, by_order, baseline;
    std::unique_ptr<llama_model> model;
    std::unique_ptr<llama_memory_hybrid_idx> mem;
    std::array<int,SEQS> stale;
    size_t cases=0;
    int guard=0;
    fixture(bool un, bool order, bool old):unified(un),by_order(order),baseline(old) {
        stale.fill(INT32_MAX);
        model.reset(llama_model_create(LLM_ARCH_QWEN4EXP,llama_model_default_params()));
        auto & h=model->hparams; h={}; h.n_layer_all=1; h.n_embd=8;
        h.n_embd_head_k_full=h.n_embd_head_v_full=4;
        h.n_head_arr.fill(1); h.n_head_kv_arr.fill(1);
        std::iota(h.kv_source_impl.begin(),h.kv_source_impl.end(),0);
        h.indexer_head_size=4; h.indexer_kpool=K; h.indexer_kpool_row=2;
        h.indexer_kpool_by_order=by_order; model->layers.resize(1);
        // Empty tensor filters preserve the real metadata caches and API paths,
        // while ensuring the fixture never executes a model or allocates weights.
        auto skip=[](int32_t){return false;};
        mem=std::make_unique<llama_memory_hybrid_idx>(*model,true,
            GGML_TYPE_F16,GGML_TYPE_F16,false,SIZE,0,LLAMA_SWA_TYPE_NONE,
            GGML_TYPE_F32,GGML_TYPE_F32,SEQS,SEQS,0,false,unified,skip,skip,skip);
        auto reserve=mem->init_full();
        auto * r=dynamic_cast<llama_memory_hybrid_idx_context *>(reserve.get());
        need(r && r->get_n_kpool()>=SIZE/K*SEQS,"reservation worst case");
    }
    std::array<std::vector<cell>,SEQS> cells() const {
        std::array<std::vector<cell>,SEQS> result;
        for (int s=0;s<SEQS;++s) {
            auto & actual=mem->get_mem_idx()->get_cells(s);
            // Independent enumeration of live cells, not the production layout.
            for (uint32_t c=0;c<actual.size();++c)
                if (!actual.is_empty(c) && actual.seq_get_all(c).test(s)) result[s].emplace_back(actual.pos_get(c),c);
            std::sort(result[s].begin(),result[s].end());
        }
        return result;
    }
    std::vector<pool> pools(const std::array<std::vector<cell>,SEQS> & all) const {
        std::vector<pool> out;
        for (int s=0;s<SEQS;++s) {
            auto & v=all[s];
            for (size_t j=0;j+K<=v.size();) {
                bool valid=by_order || (v[j].first-v.front().first)%K==0;
                if (!by_order) for (int k=1;k<K;++k) valid &= v[j+k].first==v[j].first+k;
                if (valid) { out.push_back({s,unified?0:s,{v.begin()+j,v.begin()+j+K}}); j+=K; }
                else ++j;
            }
        }
        return out;
    }
    void run(const char * label, llama_ubatch u, bool half=false, bool select=true, bool positions=true) {
        auto attn=mem->get_mem_attn()->prepare({u});
        need(!attn.empty(),"attention prepare");
        need(mem->get_mem_recr()->prepare({u}),"recurrent prepare");
        auto idx=attn;
        llama_memory_hybrid_idx_context actual(mem.get(),attn,idx,{u});
        if(guard==1) {actual.get_n_kpool();throw std::runtime_error("pre-apply guard was bypassed");}
        need(actual.apply(),"production context apply");
        auto all=cells(); auto pp=pools(all);
        std::set<int> active(u.seq_id_unq,u.seq_id_unq+u.n_seqs_unq);
        std::vector<pool> scored;
        for (auto & p:pp) if (baseline || active.count(p.seq)) scored.push_back(p);
        uint32_t n=std::max<size_t>(64,((scored.size()+1+63)/64)*64);
        need(actual.get_n_kpool()==n,"active pool graph shape "+std::string(label));
        uint32_t ng=actual.get_n_kpool_new();
        int sentinel=actual.get_idx()->get_n_kv();
        // Determine new ownership independently: stale suffixes first, then
        // touched tokens. Shared representatives acquire exactly one owner.
        std::map<int64_t,size_t> owner;
        for (size_t j=0;j<pp.size();++j) if (pp[j].members.back().first>=stale[pp[j].seq]) owner.emplace(pp[j].rep(),j);
        for (uint32_t i=0;i<u.n_tokens;++i) for (int k=0;k<u.n_seq_id[i];++k) {
            int s=u.seq_id[i][k];
            for (size_t j=0;j<pp.size();++j) if (pp[j].seq==s) {
                bool hit=false;
                if (by_order) {
                    uint32_t c=idx[0].idxs[i/idx[0].size()][i%idx[0].size()];
                    for (auto m:pp[j].members) hit |= m==cell(u.pos[i],c);
                } else hit=pp[j].members.front().first<=u.pos[i] && u.pos[i]<=pp[j].members.back().first;
                if (hit) owner.emplace(pp[j].rep(),j);
            }
        }
        std::vector<size_t> fresh; for (auto [rep,j]:owner) fresh.push_back(j);
        std::sort(fresh.begin(),fresh.end());
        size_t bound=u.n_tokens/K+u.n_seqs_unq;
        size_t maxp=SIZE/K*SEQS;
        size_t npadall=std::max<size_t>(64,((pp.size()+1+63)/64)*64);
        need(ng==std::max({fresh.size(),size_t(1),std::min({bound,npadall-1,maxp})}),"new graph bound");
        tensor pc(GGML_TYPE_I32,n), pi(GGML_TYPE_I32,K,n), pm(half?GGML_TYPE_F16:GGML_TYPE_F32,n,u.n_tokens);
        tensor ti(GGML_TYPE_I32,K-1,u.n_tokens), sm(GGML_TYPE_F32,3*K+K-1,1,1,u.n_tokens);
        tensor ni(GGML_TYPE_I32,K,ng), nr(GGML_TYPE_I64,ng), pos(GGML_TYPE_I32,4*ng);
        if(guard==2) ++pm.t->ne[0];
        if(guard==3) ++pi.t->ne[0];
        actual.set_input_kpool(pc.t,pi.t,pm.t,ti.t,select?sm.t:nullptr,ni.t,guard==4?nullptr:nr.t,&u,positions?pos.t:nullptr);
        auto eq=[](int64_t a,int64_t b,const char * s){need(a==b,std::string(s)+" got="+std::to_string(a)+" expected="+std::to_string(b));};
        int firstseq=u.seq_id[0][0]; int64_t dummy=-1;
        for (auto c:all[firstseq]) if (c.first==u.pos[0]) {dummy=(unified?0:firstseq)*SIZE+c.second; break;}
        need(dummy>=0,"dummy oracle");
        for (size_t j=0;j<n;++j) {
            eq(pc.at<int32_t>(j),j<scored.size()?scored[j].rep():dummy,"pool representative");
            for (int k=0;k<K;++k) eq(pi.at<int32_t>(j*K+k),j<scored.size() && (unified || active.count(scored[j].seq))?scored[j].members[k].second:sentinel,"pool cell");
        }
        std::set<int64_t> reps, writes;
        for (auto & p:pp) reps.insert(p.rep());
        int64_t nextpad=0;
        for (size_t j=0;j<ng;++j) {
            bool real=j<fresh.size();
            if (!real) while (reps.count(nextpad)) ++nextpad;
            int64_t rep=real?pp[fresh[j]].rep():nextpad++;
            eq(nr.at<int64_t>(j),rep,"new representative");
            need(writes.insert(rep).second,"duplicate scatter ownership");
            need(real || !reps.count(rep),"padding overwrites live representative including idle sequence");
            for (int k=0;k<K;++k) eq(ni.at<int32_t>(j*K+k),real?pp[fresh[j]].stream*SIZE+pp[fresh[j]].members[k].second:rep,"new member");
            if (positions) {
                int values[4]={};
                if (real) {
                    auto & p=pp[fresh[j]]; auto e=mem->get_mem_idx()->get_cells(p.seq).ext_get(p.members.front().second);
                    values[0]=values[3]=p.members.front().first; values[1]=e.y; values[2]=e.x;
                }
                for(int k=0;k<4;++k) eq(pos.at<int32_t>(k*ng+j),values[k],"pool M-RoPE position");
            }
        }
        for (uint32_t i=0;i<u.n_tokens;++i) {
            int s=u.seq_id[i][0]; int r=-1;
            uint32_t c=idx[0].idxs[i/idx[0].size()][i%idx[0].size()];
            for (size_t j=0;j<all[s].size();++j) if (all[s][j]==cell(u.pos[i],c)) r=j;
            need(r>=0,"token rank");
            size_t visible=0;
            for (size_t j=0;j<n;++j) {
                bool keep=false;
                if (j<scored.size() && scored[j].seq==s) {
                    auto & p=scored[j];
                    if (by_order) { auto end=std::find(all[s].begin(),all[s].end(),p.members.back()); keep=end-all[s].begin()<=r; }
                    else keep=p.members.back().first<=u.pos[i];
                }
                visible+=keep;
                float value=half?ggml_fp16_to_fp32(pm.at<ggml_fp16_t>(i*n+j)):pm.at<float>(i*n+j);
                need(keep?value==0:std::isinf(value)&&value<0,"pool causal/sequence mask");
            }
            int tail=by_order?(r+1)%K:(u.pos[i]-all[s].front().first+1)%K;
            for(int k=0;k<K-1;++k) {
                int expected=sentinel;
                if (k<tail) {
                    if (by_order) expected=all[s][r-k].second;
                    else for(auto v:all[s]) if(v.first==u.pos[i]-k) {expected=v.second;break;}
                }
                eq(ti.at<int32_t>(i*(K-1)+k),expected,"tail cells");
                if(select) { float v=sm.at<float>(i*(4*K-1)+3*K+k); need(expected!=sentinel?v==0:std::isinf(v)&&v<0,"tail selection mask"); }
            }
            if(select) for(size_t k=0;k<3*K;++k) {float v=sm.at<float>(i*(4*K-1)+k);need(k<std::min<size_t>(visible,3)*K?v==0:std::isinf(v)&&v<0,"selection pool mask");}
        }
        for(auto t:{&pc,&pi,&pm,&ti,&sm,&ni,&nr,&pos}) t->guards();
        // This is the same lifecycle boundary normal ubatch iteration uses.
        actual.next(); stale.fill(INT32_MAX); ++cases;
        std::printf("PASS %s unified=%d by_order=%d mask=%s scored=%zu all=%zu fresh=%zu padded=%u\n",label,unified,by_order,half?"F16":"F32",scored.size(),pp.size(),fresh.size(),ng);
    }
    void cp(int a,int b) {mem->seq_cp(a,b,0,-1);stale[b]=0;}
    void rm(int s,int p) {need(mem->seq_rm(s,p,-1),"seq_rm");stale[s]=std::min(stale[s],p);}
    void shift(int s,int p,int delta) {mem->seq_add(s,p,-1,delta);stale[s]=std::min(stale[s],p+std::min(delta,0));}
};

int main(int argc,char **argv) {
    try {
        bool baseline=argc==2 && std::string(argv[1])=="--baseline";
        bool guards=argc==2 && std::string(argv[1])=="--guards";
        need(argc==1 || baseline || guards,"usage: test-active-kpool [--baseline|--guards]");
        // Only the CPU backend is initialized. No model is loaded or decoded.
        auto cpu=ggml_backend_cpu_init(); need(cpu!=nullptr,"CPU backend initialization");
        if(guards) {
            for(int g=1;g<=4;++g) {
                pid_t pid=fork();need(pid>=0,"fork guard probe");
                if(pid==0) {
                    fixture f(true,true,false); f.guard=g;
                    f.run("guard",batch({{0}},0,4));
                    _exit(73);
                }
                int status=0;need(waitpid(pid,&status,0)==pid,"wait guard probe");
                need(WIFSIGNALED(status)&&WTERMSIG(status)==SIGABRT,"expected production GGML assertion guard "+std::to_string(g));
                std::printf("PASS production assertion guard=%d\n",g);std::fflush(stdout);
            }
            ggml_backend_free(cpu);return 0;
        }
        size_t total=0;
        for(bool unified:{true,false}) for(bool order:{true,false}) {
            fixture f(unified,order,baseline);
            f.run("idle-long",batch({{0}},0,272));
            f.run("idle-second",batch({{1}},0,132),true);
            auto pads=batch({{1},{2},{3}},132,3);
            for(int j=0;j<3;++j) {pads.pos[3+j]=j;pads.pos[6+j]=j;}
            f.run("five-padded-writes-protect-idle-rep3",pads);
            f.run("active-short",batch({{2}},3,4));
            f.run("complete-pool-no-padding",batch({{2}},7,1),true,false,false);
            f.run("zero-fresh-padding",batch({{2}},8,1));
            auto multi=batch({{1},{2}},132,4);
            for(int j=0;j<4;++j) multi.pos[4+j]=9+j;
            f.run("multi-active",multi,true);
            if(unified) {
                f.cp(2,3);
                f.run("shared-after-copy",batch({{2,3}},13,4));
                f.run("shared-new-representatives",batch({{2,3}},17,5),true);
                f.cp(2,3);
                f.run("higher-active-lower-idle-shared",batch({{3}},22,4));
                f.shift(2,0,0);
                f.run("idle-owner-active-sharer",batch({{3}},26,2),true);
            }
            f.rm(1,130);
            int next=unified?22:13;
            f.run("edited-idle-repool",batch({{2}},next,2));
            f.shift(1,0,3);
            f.run("shifted-idle-repool",batch({{2}},next+2,1),true);
            f.mem->seq_keep(2); f.stale.fill(0);
            f.run("keep-sequence",batch({{2}},next+3,2));
            f.mem->clear(false); f.stale.fill(0);
            f.run("clear-new-image-cells",batch({{0}},20,13,true),true);
            f.run("image-append",batch({{0}},25,8,true));
            total+=f.cases;
        }
        std::printf("PASS actual production active-kpool API oracle cases=%zu mode=%s\n",total,baseline?"baseline":"candidate");
        ggml_backend_free(cpu);
        return 0;
    } catch(const std::exception & e) {std::fprintf(stderr,"FAIL %s\n",e.what());return 1;}
}
