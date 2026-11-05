"""Regenerate coverage_fixtures.json.gz from Strata's reasoning_repeat_coverage.

    docker run --rm --entrypoint cat ghcr.io/niko1221/strata:0.2.9-cuda13 \\
        /opt/strata/terminal/serve/server.py > server.py
    python -I gen_coverage_fixtures.py server.py coverage_fixtures.json
    gzip -9 coverage_fixtures.json
"""
import collections, json, random, re, sys
src =open(sys.argv[1], encoding="utf-8").read()
start = src.index("LOOP_CHECK_EVERY = 512")
end = src.index("def focused_recovery_prompt")
ns = {"re": re, "collections": collections}
exec(src[start:end], ns)
cov = ns["reasoning_repeat_coverage"]
rng = random.Random(728)
syll = ["ka","lo","mi","ter","on","vu","sha","re","pli","dor","an","ex","qui","zo","bel","tra"]
vocab = ["".join(rng.choice(syll) for _ in range(rng.randint(1,3))) for _ in range(800)]
punct = [",", ".", ";", ":", "(", ")", "=", "+"]
def varied(n):
    out = []
    for _ in range(n):
        out.append(rng.choice(vocab))
        if rng.random() < 0.12: out.append(rng.choice(punct))
    return " ".join(out)
NL = "\n"
passage = varied(260)
short_passage = varied(140)
uni_vocab = ["perché", "città", "Ωmega", "naïve", "straße", "日本語", "x²", "½", "ｆｕｌｌ", "ǅemal"]
uni_passage = " ".join(rng.choice(uni_vocab + vocab[:50]) + rng.choice(["", ",", "—", "…"]) for _ in range(200))
cases = {
  "varied_text": varied(3000),
  "repeated_passage": varied(800) + NL + NL.join([passage]*9),
  "late_short_loop": varied(2400) + NL + NL.join([short_passage]*3),
  "late_loop_over_threshold": varied(2400) + NL + NL.join([short_passage]*4),
  "two_repeats_only": varied(2400) + NL + NL.join([passage]*2),
  "periodic_tail": "Let me compute: " + "12345678901" * 400,
  "periodic_tail_mixed_case": varied(100) + " " + "AbC dE-" * 400,
  "below_window": NL.join([passage]*6),
  "unicode_loop": varied(500) + NL + NL.join([uni_passage]*6) + " tail end",
  "history_truncation": passage + NL + varied(30200) + NL + NL.join([passage]*2),
  "history_no_truncation": passage + NL + varied(26000) + NL + NL.join([passage]*2),
}
out = [{"name": k, "text": v, "coverage": cov(v)} for k, v in cases.items()]
for c in out: print(c["name"], len(c["text"]), c["coverage"])
json.dump(out, open(sys.argv[2], "w", encoding="utf-8"), ensure_ascii=False, indent=1)
