#!/usr/bin/env python3
"""Analytic counterexamples and planning assumptions; NOT Elephant benchmark results."""
import json
import math


def paired_sample_plan(delta, discordance, cluster_size=1, rho=0):
    """Normal-approximation planning only; binary paired D in {-1,0,1}."""
    if not (0 < delta <= discordance <= 1 and cluster_size >= 1 and 0 <= rho < 1):
        raise ValueError("invalid planning assumptions")
    variance = discordance - delta**2
    independent_pairs = math.ceil((1.959964 + 0.841621)**2 * variance / delta**2)
    design_effect = 1 + (cluster_size - 1)*rho
    return {"assumed_delta": delta, "assumed_discordance": discordance,
            "independent_pairs_approx": independent_pairs,
            "assumed_cluster_size": cluster_size, "assumed_rho_of_paired_difference": rho,
            "design_effect_approx": design_effect,
            "cluster_adjusted_pairs_approx": math.ceil(independent_pairs*design_effect)}


def main():
    # Two source worlds map to the same summary, but require opposite answers.
    worlds = [{"summary": "Use caching", "backfill_allowed": False},
              {"summary": "Use caching", "backfill_allowed": True}]
    assert worlds[0]["summary"] == worlds[1]["summary"]
    assert worlds[0]["backfill_allowed"] != worlds[1]["backfill_allowed"]
    # A phrase-presence contract cannot distinguish affirmation from negation.
    fragment = "capacity is 8"
    faithful = "The capacity is 8."
    contradiction = "It is false that the capacity is 8."
    assert fragment in faithful and fragment in contradiction
    # Repeating a transformed note creates no independent observation.
    independent_evidence_ids = {"incident-1" for _ in range(100)}
    assert len(independent_evidence_ids) == 1
    # Multiple looks at a nominal test inflate errors under independent nulls.
    false_positive_any = 1 - (1-0.05)**20
    # Hypothetical costs in one common unit. Dreaming must be amortized.
    maintenance, per_task_saving = 100, 2
    break_even_queries = maintenance/per_task_saving
    # Zero observed harmful events cannot establish a zero population rate.
    upper_rate_6 = 1 - 0.05**(1/6)
    zero_harm_n_for_one_percent = math.ceil(math.log(0.05)/math.log(0.99))
    report = {
        "kind": "analytic_counterexamples_and_planning_only",
        "empirical_elephant_quality_measured": False,
        "counterexamples": {
            "lossy_summary_cannot_answer_both_worlds": True,
            "substring_gate_accepts_negated_claim": True,
            "100_rewrites_independent_evidence_count": len(independent_evidence_ids),
        },
        "hypothetical_calculations": {
            "20_independent_null_tests_at_alpha_005_any_false_positive": false_positive_any,
            "maintenance_100_saving_2_break_even_queries": break_even_queries,
            "zero_harms_in_6_iid_trials_one_sided_95_upper_rate": upper_rate_6,
            "zero_harms_iid_trials_needed_for_one_sided_95_upper_rate_below_001": zero_harm_n_for_one_percent,
            "paired_success_planning": paired_sample_plan(0.05, 0.20, 5, 0.20),
        },
        "limitations": ["Planning assumptions are hypothetical, not pilot estimates.",
                        "Sample-size formulas are approximations, not a power guarantee.",
                        "Existing six fixtures are curated and cannot support binomial population inference.",
                        "Cluster-size/rho inflation is only a planning approximation."]
    }
    print(json.dumps(report, indent=2))


if __name__ == "__main__":
    main()
