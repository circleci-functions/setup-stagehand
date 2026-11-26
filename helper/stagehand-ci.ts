// stagehand-ci.ts: start Stagehand the way setup-stagehand set up the job.
//
// Copy this file into your project. It needs only @browserbasehq/stagehand 4,
// and runs as is with Node.js 22.18 or later, which strips the types.
//
//   import { withStagehand, url } from "./stagehand-ci.ts";
//
//   await withStagehand("checkout", async ({ stagehand, page }) => {
//     await page.goto(url("/"));
//     await stagehand.act("add the first product to the cart");
//   });
//
// It reads what the step exports, and works the same on a laptop:
//
//   STAGEHAND_BROWSER        local or browserbase. Unset: browserbase when
//                            BROWSERBASE_API_KEY is set, else local.
//   STAGEHAND_MODEL          <provider>/<model>, passed to Stagehand as is.
//                            Unset: a model for whichever of ANTHROPIC_API_KEY
//                            or OPENAI_API_KEY is set, and on Browserbase with
//                            neither, its model gateway. Pass { model } to
//                            withStagehand or launch to use your own client.
//   STAGEHAND_ARTIFACTS_DIR  Where screenshots of failures and Browserbase
//                            replay links go; the step stores it as artifacts.
//   BASE_URL                 The site under test, for url().

import { mkdirSync, appendFileSync } from "node:fs";
import { join } from "node:path";
import { browserbase, localBrowser, Stagehand } from "@browserbasehq/stagehand";
import type { ModelConfig, Page, StagehandBrowser, StagehandCreateOptions } from "@browserbasehq/stagehand";

// Models used when STAGEHAND_MODEL is unset, for the first key that is set.
// setup-stagehand releases update them.
const defaultModels: Array<[key: string, model: string]> = [
  ["ANTHROPIC_API_KEY", "anthropic/claude-sonnet-5-5"],
  ["OPENAI_API_KEY", "openai/gpt-5.4-mini"],
];

// Where each provider's API key is read from.
const providerKeys: Record<string, string[]> = {
  anthropic: ["ANTHROPIC_API_KEY"],
  openai: ["OPENAI_API_KEY"],
  google: ["GOOGLE_GENERATIVE_AI_API_KEY", "GEMINI_API_KEY"],
  groq: ["GROQ_API_KEY"],
  cerebras: ["CEREBRAS_API_KEY"],
};

export interface Options {
  /**
   * The model to give Stagehand, overriding STAGEHAND_MODEL and the defaults:
   * for a provider or gateway the helper does not configure itself.
   */
  model?: StagehandCreateOptions["model"];
}

export interface Session {
  browser: StagehandBrowser;
  stagehand: Stagehand;
  page: Page;
}

/** url resolves a path against BASE_URL. */
export function url(path = "/"): string {
  const base = process.env.BASE_URL;
  if (!base) {
    throw new Error("BASE_URL is not set: set base-url on the setup-stagehand step, or export it");
  }
  return new URL(path, base.endsWith("/") ? base : base + "/").toString();
}

/** launch starts a browser, locally or on Browserbase, and Stagehand on it. */
export async function launch(options: Options = {}): Promise<Session> {
  const browser = onBrowserbase() ? await launchBrowserbase() : await localBrowser.launch();
  try {
    const m = options.model ?? model();
    const stagehand = await Stagehand.create(m ? { browser, model: m } : { browser });
    const page = (await browser.context.activePage()) ?? (await browser.context.newPage());
    return { browser, stagehand, page };
  } catch (err) {
    await browser.close();
    throw err;
  }
}

/**
 * withStagehand runs fn with a fresh session, saves a full-page screenshot
 * named after the run if fn throws, and always closes the session, which
 * releases a Browserbase one. The error is rethrown, so the step fails.
 */
export async function withStagehand(
  name: string,
  fn: (session: Session) => Promise<void>,
  options: Options = {},
): Promise<void> {
  const session = await launch(options);
  try {
    await fn(session);
  } catch (err) {
    const page = (await session.browser.context.activePage().catch(() => undefined)) ?? session.page;
    await screenshot(page, name);
    throw err;
  } finally {
    await session.stagehand.close().catch(() => {});
    await session.browser.close();
  }
}

/** screenshot saves a full-page screenshot to the artifacts directory. */
export async function screenshot(page: Page, name: string): Promise<string | undefined> {
  const path = join(artifactsDir(), name.replace(/[^\w.-]+/g, "-") + ".png");
  try {
    await page.screenshot({ path, fullPage: true });
    console.error(`stagehand-ci: saved a screenshot to ${path}`);
    return path;
  } catch (err) {
    console.error(`stagehand-ci: could not save a screenshot: ${err}`);
    return undefined;
  }
}

function onBrowserbase(): boolean {
  const where = process.env.STAGEHAND_BROWSER;
  if (where === "browserbase" || where === "local") {
    return where === "browserbase";
  }
  return Boolean(process.env.BROWSERBASE_API_KEY);
}

async function launchBrowserbase(): Promise<StagehandBrowser> {
  const apiKey = process.env.BROWSERBASE_API_KEY;
  if (!apiKey) {
    throw new Error("BROWSERBASE_API_KEY is not set");
  }
  // Browserbase rejects empty metadata values, so unset ones are left out.
  const ci: Record<string, string> = { provider: "circleci" };
  for (const [key, name] of [["pipeline", "CIRCLE_PIPELINE_ID"], ["job", "CIRCLE_WORKFLOW_JOB_ID"], ["commit", "CIRCLE_SHA1"]]) {
    const value = process.env[name];
    if (value) {
      ci[key] = value;
    }
  }
  const projectId = process.env.BROWSERBASE_PROJECT_ID;
  const browser = await browserbase.launch({ apiKey, ...(projectId ? { projectId } : {}), userMetadata: { ci } });
  if (browser.sessionId) {
    const replay = `https://www.browserbase.com/sessions/${browser.sessionId}`;
    console.error(`stagehand-ci: Browserbase session replay: ${replay}`);
    appendFileSync(join(artifactsDir(), "replays.txt"), replay + "\n");
  }
  return browser;
}

function model(): ModelConfig | undefined {
  let name = process.env.STAGEHAND_MODEL;
  if (!name) {
    name = defaultModels.find(([key]) => process.env[key])?.[1];
  }
  if (!name) {
    return undefined;
  }
  const provider = name.split("/")[0];
  const key = (providerKeys[provider] ?? []).find((k) => process.env[k]);
  console.error(`stagehand-ci: using ${name}${key ? ` with ${key}` : ""}`);
  const modelName = name as ModelConfig["modelName"];
  return key ? { modelName, apiKey: process.env[key] } : { modelName };
}

function artifactsDir(): string {
  const dir = process.env.STAGEHAND_ARTIFACTS_DIR || "stagehand-artifacts";
  mkdirSync(dir, { recursive: true });
  return dir;
}
