# setup-stagehand

A CircleCI function that runs your [Stagehand](https://stagehand.dev) scripts and tests in a job:
it puts a Chrome that Stagehand can drive on the job, with its system packages, cached across
jobs, or runs on [Browserbase](https://www.browserbase.com) instead, and keeps screenshots of
failures as artifacts. The script you run on your laptop runs unchanged.

```yaml
functions:
  stagehand: github.com/circleci-functions/setup-stagehand@v0.1.0

jobs:
  e2e:
    docker:
      - image: cimg/node:lts
    steps:
      - checkout
      - run: npm ci
      - stagehand:                       # same as stagehand/run
          with:
            command: node e2e/checkout.ts
```

Run `circleci function add github.com/circleci-functions/setup-stagehand --as stagehand` to write
the entry.

Put the model provider's key, such as `ANTHROPIC_API_KEY`, in a context. Set `BROWSERBASE_API_KEY`
as well to run the browser on Browserbase.

## run

Checks the job has what the command needs, puts Chrome on the job (unless it runs on Browserbase),
then runs `command` in `working-directory`.

| Flag | Default | Description |
|---|---|---|
| `command` | | Required. Shell command that runs your Stagehand scripts or tests |
| `base-url` | `$DEPLOYMENT_URL`, then `$VERCEL_DEPLOYMENT_URL` | Site to test, exported to the command as `BASE_URL` |
| `browser` | `auto` | `local`, `browserbase`, or `auto`: Browserbase when `BROWSERBASE_API_KEY` is set |
| `model` | | `<provider>/<model>`, such as `anthropic/claude-sonnet-5-5`, exported as `STAGEHAND_MODEL` |
| `test-results` | | JUnit XML file or directory the command writes, stored in the Tests tab |
| `working-directory` | `.` | Directory to run the command in |

Plus the [`install`](#install) flags.

A `base-url` without a scheme gets `https://`, or `http://` for `localhost` and `127.0.0.1`.

Before installing anything, the step fails with what to fix if:

- `model` is set and its provider's key is not (on Browserbase the key is optional, as
  Browserbase can serve the model);
- `browser: browserbase` is set and `BROWSERBASE_API_KEY` is not;
- the project uses Stagehand 4 and the job's Node.js is older than 22.18, which it needs.

It warns when no model key is set at all, since `act`, `extract`, `observe` and `agent` need one.

The command runs with bash (or `sh`), and gets these variables:

| Variable | |
|---|---|
| `CHROME_PATH` | The Chrome to launch, which Stagehand reads. Not set on Browserbase |
| `BASE_URL` | The site under test |
| `STAGEHAND_BROWSER` | `local` or `browserbase` |
| `STAGEHAND_MODEL` | `model`, when set |
| `STAGEHAND_ARTIFACTS_DIR` | A directory stored as artifacts under `stagehand/` after the command, pass or fail |

The step fails if the command does.

## The helper

Stagehand reads no variables except `CHROME_PATH`, so a script picks its browser and model itself.
[`helper/stagehand-ci.ts`](helper/stagehand-ci.ts) does it from the variables above. Copy it into
your project. It needs only `@browserbasehq/stagehand` 4, and Node.js 22.18 or later runs it
as it is:

```ts
import { withStagehand, url } from "./stagehand-ci.ts";

await withStagehand("checkout", async ({ stagehand, page }) => {
  await page.goto(url("/"));
  await stagehand.act("add the first product to the cart");
  await stagehand.act("open the cart and check out");
});
```

`withStagehand`:

- launches Chrome locally, or a Browserbase session tagged with the pipeline, job and commit, whose
  replay link it prints and saves to `replays.txt` in the artifacts;
- uses `STAGEHAND_MODEL`, or with none, `anthropic/claude-sonnet-5-5` when `ANTHROPIC_API_KEY` is
  set, then `openai/gpt-5.4-mini` when `OPENAI_API_KEY` is, and prints the one it picked. On
  Browserbase with neither, Browserbase picks the model. New releases of setup-stagehand update
  these defaults, so a model changes only when you upgrade;
- saves a full-page screenshot to the artifacts if the callback throws, then rethrows;
- always closes the browser, which releases a Browserbase session.

`launch()` and `screenshot()` are there for test runners that manage their own lifecycle. On a
laptop the helper works the same way, reading the same variables.

Model keys are read from `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`,
`GOOGLE_GENERATIVE_AI_API_KEY` (or `GEMINI_API_KEY`), `GROQ_API_KEY` and `CEREBRAS_API_KEY`, the
providers Stagehand supports.

## install

Puts a Chrome that Stagehand can drive on the job, and exports `CHROME_PATH` to later steps.

| Flag | Default | Description |
|---|---|---|
| `chrome-version` | | Empty uses the job's own Chrome, where Stagehand would find it, and installs Chrome for Testing stable if there is none. A channel (`stable`, `beta`, `dev`, `canary`), a milestone (`"158"`) or a full version installs that Chrome for Testing |
| `with-deps` | `true` | On Debian and Ubuntu, install the system packages a downloaded Chrome needs, with `sudo`. Packages already installed are skipped |
| `cache` | `true` | Restore the downloaded Chrome from the job cache, and save it after a fresh download |

```yaml
      - stagehand/install
      - run: npm test
```

[Chrome for Testing](https://developer.chrome.com/blog/chrome-for-testing) is Google's build of
Chrome for automation. It is cached keyed by the platform and version, so a job downloads it once
per version. Its system packages are read from the list the build ships with, and cannot be
cached. A failed download or package install is tried twice more, 15 and then 30 seconds later.

On Linux with no display, `CHROME_PATH` points at a small wrapper that runs Chrome headless, so a
script that launches Chrome with a window still works.

## Platforms

Linux (amd64, arm64), macOS and Windows. `with-deps` covers Debian and Ubuntu; on other Linux
images, install Chrome's libraries yourself. The helper needs Stagehand 4; Stagehand 3 scripts
can use `run` and `install` without it.

## Releasing

See [RELEASE.md](RELEASE.md).
