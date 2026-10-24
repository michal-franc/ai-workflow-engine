You are reviewing the health of the project {{project}} ({{languages}}). The owner wants feedback to stay fast and the code to stay lean: fewer, faster, sharper tests where unit tests are redundant or slow; end-to-end sessions where real flows have none; smaller files and functions where change concentrates. More tests is not automatically better.

Read the evidence pack first: {{pack}}

Then sample the code in {{root}}: open the 3 biggest hotspots, the 5 slowest tests and the 2 least-tested areas, and at least one test file per area you recommend changing. Don't edit anything, don't run the full suite, and don't create issues.

Write a JSON file matching `issue-cli health review --schema` and record it with:

    issue-cli --project {{slug}} health review record <file>

Include:
- score: 0–100, your judgement of how healthy the project is for agents and the owner to keep changing it. Say in the assessment where you differ from the derived score ({{derived}}) and why.
- assessment: 60–200 words. What is getting better, what is getting worse, the one thing to fix first.
- recommendations: 1–6, highest value first. Each has an id (R1, R2, ...), a kind (remove_tests, merge_tests, speed_up_tests, add_e2e, split_file, simplify, fix_flaky, config, other), a title under 80 characters, evidence (at least one metric id copied from the evidence pack, or one repo path with an optional line range, and what it shows), an effort (S ≤ ½ day, M ≤ 2 days, L more) and expected_effect (which metric moves, and by roughly how much).

Don't repeat a recommendation that is already filed or dismissed unless its evidence got worse; say so if it did. Prefer one precise recommendation ("these 12 tests in X assert the same Y and cost 30 s") over a general one ("improve tests").

`record` refuses a file whose evidence points at a metric or path that doesn't exist, and lists each problem with its fix: correct the file and record it again. Nothing becomes an issue: the owner files recommendations from the Health view.
