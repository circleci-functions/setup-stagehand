import { withStagehand, url } from "./stagehand-ci.ts";

await withStagehand("checkout page", async ({ page }) => {
  await page.goto(url("/"));
  throw new Error("the checkout button is missing");
});
