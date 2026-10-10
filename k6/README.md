# Load tests (k6)

Install k6: <https://grafana.com/docs/k6/latest/set-up/install-k6/> (on Windows: `winget install k6`).

Both scripts need the deployed API URL and a Cognito **ID token**. To get a token, sign in to the
frontend as the right user, open the browser dev tools **Network** tab, click any API request, and
copy its `Authorization` header without the `Bearer ` prefix. Tokens expire after 60 minutes.

| Script | What it shows | Command |
|---|---|---|
| `resources-read.js` | Read throughput and latency on `/resources` with the 15 s cache | `k6 run -e API_URL=… -e TOKEN=… k6/resources-read.js` |
| `admission-burst.js` | 20 simultaneous admissions: exactly *free-units* succeed, the rest get 409, availability never negative | `k6 run -e API_URL=… -e TOKEN=<doctor.h001> -e HOSPITAL=H001 -e TYPE=ICU_BED k6/admission-burst.js` |

Both are sized to stay well within the free tier and the API throttle (25 req/s).
Save summaries for the report with `--summary-export k6/results/<name>.json`. That folder is git-ignored.
