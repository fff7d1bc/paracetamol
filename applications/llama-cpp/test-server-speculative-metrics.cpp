#include "server-common.h"

#include <cstdint>
#include <cstdio>
#include <limits>
#include <string>

// Matching-source CPU serialization fixture. Link the actual server-common.cpp
// implementation (or its server library); no model, context or backend starts.
static bool exact_unsigned(const json & value, const char * key, uint64_t expected) {
    return value.contains(key) && value[key].is_number_integer() && value[key].get<uint64_t>() == expected &&
        value[key].dump() == std::to_string(expected);
}

static bool ordinary_fields(const json & value) {
    return value.size() == 9 && value.contains("cache_n") && value.contains("prompt_n") &&
        value.contains("prompt_ms") && value.contains("prompt_per_token_ms") &&
        value.contains("prompt_per_second") && value.contains("predicted_n") &&
        value.contains("predicted_ms") && value.contains("predicted_per_token_ms") &&
        value.contains("predicted_per_second");
}

int main() {
    int failures = 0;
    const auto check = [&](const char * name, bool valid) {
        std::printf("%s: %s\n", name, valid ? "PASS" : "FAIL");
        failures += !valid;
    };

    server_slot_stats stats;
    check("ordinary speculative keys absent", ordinary_fields(stats.to_json()));

    stats.n_draft_accepted = 17;
    stats.n_draft_verif_steps = 6;
    check("no draft tokens do not infer verification", ordinary_fields(stats.to_json()));

    stats.n_draft_tokens = 18;
    stats.n_draft_accepted = 12;
    const auto active = stats.to_json();
    check("six exact verification rounds", active.size() == 12 &&
        exact_unsigned(active, "draft_n", 18) && exact_unsigned(active, "draft_n_accepted", 12) &&
        exact_unsigned(active, "draft_verif_steps", 6));

    stats.n_draft_verif_steps = 0;
    check("positive drafts retain explicit zero rounds",
        exact_unsigned(stats.to_json(), "draft_verif_steps", 0));

    stats.n_draft_tokens = std::numeric_limits<uint64_t>::max();
    stats.n_draft_accepted = (uint64_t{1} << 53) + 1;
    stats.n_draft_verif_steps = std::numeric_limits<uint64_t>::max() - 1;
    const auto wire = stats.to_json().dump();
    const auto decoded = json::parse(wire);
    check("large unsigned wire values remain exact",
        exact_unsigned(decoded, "draft_n", stats.n_draft_tokens) &&
        exact_unsigned(decoded, "draft_n_accepted", stats.n_draft_accepted) &&
        exact_unsigned(decoded, "draft_verif_steps", stats.n_draft_verif_steps) &&
        wire.find("\"draft_verif_steps\":18446744073709551614") != std::string::npos);

    check("serialization does not mutate verification counter",
        stats.n_draft_verif_steps == std::numeric_limits<uint64_t>::max() - 1);
    return failures == 0 ? 0 : 1;
}
