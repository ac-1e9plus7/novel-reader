import { test, expect } from "@playwright/test";
import type { Chapter, Job, Progress } from "../src/types";
test("import, translate, correct, synthesize, reload and preserve reading state", async ({
  page,
}) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: /让每个故事/ })).toBeVisible();
  await page.getByRole("button", { name: "创建小说", exact: true }).click();
  await page.getByLabel("小说名称").fill("夏日来信 " + Date.now());
  await page.getByLabel("作者（选填）").fill("测试作者");
  await page.getByRole("button", { name: "保存小说" }).click();
  await page
    .locator("main")
    .getByRole("button", { name: "添加章节", exact: true })
    .click();
  await page.getByLabel("章节标题").fill("第一章 窓辺");
  await page
    .getByLabel("日文原文")
    .fill(
      "窓を開けると、風が吹いていた。\n「おはよう」と彼女は言った。\n新しい一日が始まった。",
    );
  await page.getByText("分段预览", { exact: true }).click();
  await expect(page.locator(".preview p")).toHaveCount(3);
  await page.getByRole("button", { name: "保存章节" }).click();
  await expect(page.locator(".segment")).toHaveCount(3);
  await page.getByRole("button", { name: "翻译第1段", exact: true }).click();
  await expect(page.locator(".translation p").first()).toContainText(
    "窗外的风",
  );
  await expect(page.locator(".untranslated")).toHaveCount(2);
  await page.getByRole("button", { name: "校对译文" }).first().click();
  await page.getByLabel("中文译文", { exact: true }).fill("我校对的第一段。");
  await page.getByRole("button", { name: "保存修改" }).click();
  await expect(page.locator(".translation p").first()).toHaveText(
    "我校对的第一段。",
  );
  await page.getByRole("button", { name: "准备整章语音" }).click();
  await expect(page.locator(".audio-ready")).toHaveCount(3);
  await page.getByRole("button", { name: "朗读第1段", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "暂停", exact: true }),
  ).toBeVisible();
  await expect(page.getByLabel("播放速度")).toHaveValue("1");
  expect(
    await page
      .locator("audio")
      .evaluate((a: HTMLAudioElement) => a.playbackRate),
  ).toBe(1);
  await page.getByRole("button", { name: "暂停", exact: true }).click();
  await page.getByLabel("播放速度").selectOption("0.8");
  await page.getByRole("button", { name: "调整读音" }).first().click();
  await page
    .getByLabel("朗读文本", { exact: true })
    .fill("まどをあけると、かぜがふいていた。");
  await page.getByRole("button", { name: "保存修改" }).click();
  await expect(page.locator(".audio-ready")).toHaveCount(2);
  await expect(page.locator(".japanese p").first()).toHaveText(
    "窓を開けると、風が吹いていた。",
  );
  await page.evaluate(() => window.scrollTo({ top: 0, behavior: "instant" }));
  await page.screenshot({
    path: `test-results/reader-${test.info().project.name}.png`,
    fullPage: true,
  });
  await page.reload();
  await page
    .locator(".book-card")
    .filter({ hasText: "夏日来信" })
    .last()
    .click();
  await expect(page.locator(".translation p").first()).toHaveText(
    "我校对的第一段。",
  );
  await expect(page.locator(".segment")).toHaveCount(3);
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth > innerWidth,
  );
  expect(overflow).toBeFalsy();
});
test("glossary editing, source revision, theme and mode remain usable", async ({
  page,
  request,
}) => {
  const headers = { "X-Reader-Request": "1" };
  const b = await (
    await request.post("/api/books", {
      headers,
      data: { title: "术语测试 " + Date.now(), author: "" },
    })
  ).json();
  await request.post(`/api/books/${b.id}/chapters`, {
    headers,
    data: { title: "第一章", raw: "夏目は笑った。" },
  });
  await page.goto("/");
  await page
    .getByRole("button")
    .filter({ has: page.getByRole("heading", { name: b.title, exact: true }) })
    .click();
  if (test.info().project.name === "mobile")
    await page.getByRole("button", { name: "章节目录" }).click();
  await page.getByRole("button", { name: "人名与术语表" }).click();
  await page.getByRole("button", { name: "添加术语" }).click();
  await page.getByLabel("1-source", { exact: true }).fill("夏目");
  await page.getByLabel("1-target", { exact: true }).fill("夏目");
  await page.getByLabel("1-reading", { exact: true }).fill("なつめ");
  await page.getByRole("button", { name: "保存术语表" }).click();
  if (test.info().project.name === "mobile")
    await page.getByRole("button", { name: "关闭目录" }).click();
  await page.getByRole("button", { name: "编辑原文" }).click();
  await page.getByLabel("日文原文").fill("夏目は微笑んだ。\n雨が止んだ。");
  await page.getByRole("button", { name: "保存章节" }).click();
  await expect(page.locator(".segment")).toHaveCount(2);
  await page.getByRole("button", { name: "日文", exact: true }).click();
  await expect(page.locator(".translation")).toHaveCount(0);
  await page.getByRole("button", { name: "切换主题" }).click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await page.getByRole("button", { name: "设置", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "阅读与服务设置" }),
  ).toBeVisible();
});

test("resume a saved audio position and play through subsequent clips", async ({
  page,
  request,
}) => {
  const headers = { "X-Reader-Request": "1" };
  const b = await (
    await request.post("/api/books", {
      headers,
      data: { title: "断点续播 " + Date.now(), author: "" },
    })
  ).json();
  const created = await (
    await request.post(`/api/books/${b.id}/chapters`, {
      headers,
      data: { title: "连续播放", raw: "一日目。\n二日目。\n三日目。" },
    })
  ).json();
  const j: Job = await (
    await request.post(`/api/chapters/${created.id}/audio-jobs`, {
      headers,
      data: { revision: 1, segments: [], force: false },
    })
  ).json();
  await expect
    .poll(
      async () =>
        (await (await request.get(`/api/jobs/${j.id}`)).json()).status,
    )
    .toBe("succeeded");
  const c: Chapter = await (
    await request.get(`/api/chapters/${created.id}`)
  ).json();
  const clip = c.segments[1].clips[0];
  const p: Progress = {
    chapter_id: c.id,
    segment_id: clip.segment_id,
    clip_id: clip.id,
    seconds: 0.3,
    revision: 0,
  };
  await request.put(`/api/books/${b.id}/progress`, { headers, data: p });
  await page.goto("/");
  await page.locator(".book-card").filter({ hasText: b.title }).click();
  await expect(page.locator(".player-label strong")).toHaveText(
    "第 2 段 · 日语朗读",
  );
  await expect
    .poll(() =>
      page.locator("audio").evaluate((a: HTMLAudioElement) => a.currentTime),
    )
    .toBeCloseTo(0.3, 1);
  expect(
    await page.locator("audio").evaluate((a: HTMLAudioElement) => a.paused),
  ).toBe(true);
  await page.getByRole("button", { name: "播放", exact: true }).click();
  await expect(page.locator(".player-label strong")).toHaveText(
    "第 3 段 · 日语朗读",
  );
  await expect(page.getByRole("status")).toContainText("本章已播放完毕");
  await expect
    .poll(
      async () =>
        (await (await request.get(`/api/books/${b.id}/progress`)).json())
          .clip_id,
    )
    .toBe(c.segments[2].clips[0].id);
  await expect(page.getByRole("alert")).toHaveCount(0);
});
