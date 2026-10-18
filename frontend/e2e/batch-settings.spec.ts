import { test, expect } from "@playwright/test";
import type { Chapter, Job } from "../src/types";

test("whole chapter translation is a batch of single-paragraph jobs and preserves manual text", async ({
  page,
  request,
}) => {
  const headers = { "X-Reader-Request": "1" };
  const b = await (
    await request.post("/api/books", {
      headers,
      data: { title: "批量单段 " + Date.now(), author: "" },
    })
  ).json();
  const created = await (
    await request.post(`/api/books/${b.id}/chapters`, {
      headers,
      data: { title: "整章翻译测试", raw: "一。\n二。\n三。\n四。" },
    })
  ).json();
  const c: Chapter = await (
    await request.get(`/api/chapters/${created.id}`)
  ).json();
  await request.patch(`/api/segments/${c.segments[0].id}/translation`, {
    headers,
    data: { revision: 0, text: "手工保留的译文" },
  });
  await page.goto("/");
  await page.locator(".book-card").filter({ hasText: b.title }).click();
  await page.getByRole("button", { name: "整章翻译", exact: true }).click();
  await expect(
    page.getByRole("region", { name: "整章翻译进度" }),
  ).toContainText("3/3 段");
  await expect(page.locator(".untranslated")).toHaveCount(0);
  await expect(page.locator(".translation p").first()).toHaveText(
    "手工保留的译文",
  );
  const jobs: Job[] = await (
    await request.get(`/api/jobs?chapter=${created.id}`)
  ).json();
  expect(jobs).toHaveLength(3);
  expect(
    jobs.every(
      (j) =>
        j.kind === "translation" && j.total === 1 && j.status === "succeeded",
    ),
  ).toBe(true);
  await page.getByRole("button", { name: "整章翻译", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("无需补充翻译");
  expect(
    await (await request.get(`/api/jobs?chapter=${created.id}`)).json(),
  ).toHaveLength(3);
});

test("advanced audio concurrency can be saved and restored without changing playback speed", async ({
  page,
  request,
}) => {
  const headers = { "X-Reader-Request": "1" };
  await request.patch("/api/settings/audio", {
    headers,
    data: { concurrency: 10 },
  });
  await page.goto("/");
  await page.getByRole("button", { name: "设置", exact: true }).click();
  await page.getByText("高级设置", { exact: true }).click();
  await expect(page.getByLabel("语音生成并发量", { exact: true })).toHaveValue(
    "10",
  );
  await page.getByLabel("语音生成并发量", { exact: true }).fill("3");
  await page.getByRole("button", { name: "保存并发设置" }).click();
  await expect(page.getByRole("dialog").getByRole("status")).toContainText(
    "已保存",
  );
  expect(
    (await (await request.get("/api/config")).json()).tts_concurrency,
  ).toBe(3);
  await page.reload();
  await page.getByRole("button", { name: "设置", exact: true }).click();
  await page.getByText("高级设置", { exact: true }).click();
  await expect(page.getByLabel("语音生成并发量", { exact: true })).toHaveValue(
    "3",
  );
  await page.getByLabel("语音生成并发量", { exact: true }).fill("33");
  await expect(
    page.getByRole("button", { name: "保存并发设置" }),
  ).toBeDisabled();
  await request.patch("/api/settings/audio", {
    headers,
    data: { concurrency: 10 },
  });
});
