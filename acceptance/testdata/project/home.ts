import { withStagehand, url } from "./stagehand-ci.ts";

await withStagehand("home", async ({ page }) => {
  await page.goto(url("/"));
  const title = await page.title();
  if (title !== "setup-stagehand acceptance") {
    throw new Error(`unexpected title ${JSON.stringify(title)}`);
  }
  console.log(`PASS: loaded ${title}`);
});
