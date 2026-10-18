import { test, expect } from "@playwright/test";
import type { Chapter, Job } from "../src/types";

test("manual and audio workflows work with a failed translator and partially empty translations", async ({
  page,
  request,
}) => {
  const headers = { "X-Reader-Request": "1" };
  const book = await (
    await request.post("/api/books", {
      headers,
      data: { title: "手动阅读 " + Date.now(), author: "" },
    })
  ).json();
  const created = await (
    await request.post(`/api/books/${book.id}/chapters`, {
      headers,
      data: {
        title: "没有译文也能读",
        raw: "風が吹いた。\n本を開いた。\n夜になった。",
      },
    })
  ).json();
  let generationRequests = 0;
  await page.route("**/api/chapters/*/translation-jobs", async (route) => {
    generationRequests++;
    await route.fulfill({ status: 503, json: { error: "翻译服务暂时不可用" } });
  });
  await page.goto("/");
  await page.locator(".book-card").filter({ hasText: book.title }).click();
  await page.getByRole("button", { name: "翻译第1段", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("翻译服务暂时不可用");
  await page.getByRole("button", { name: "准备整章语音" }).click();
  await expect(page.locator(".audio-ready")).toHaveCount(3);
  await expect(page.locator(".untranslated")).toHaveCount(3);
  await page.getByRole("button", { name: "朗读第1段", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "暂停", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "暂停", exact: true }).click();
  const before: Chapter = await (
    await request.get(`/api/chapters/${created.id}`)
  ).json();
  await page.getByRole("button", { name: "日文", exact: true }).click();
  await page.getByRole("button", { name: "手动输入译文", exact: true }).click();
  await page
    .getByLabel("第 1 段中文译文", { exact: true })
    .fill("风吹了过来。");
  await page
    .getByLabel("第 3 段中文译文", { exact: true })
    .fill("夜幕降临了。");
  await page.getByRole("button", { name: "保存已填写译文" }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await page.getByRole("button", { name: "对照", exact: true }).click();
  await expect(page.locator(".translation p").first()).toHaveText(
    "风吹了过来。",
  );
  await expect(page.locator(".untranslated")).toHaveCount(1);
  const after: Chapter = await (
    await request.get(`/api/chapters/${created.id}`)
  ).json();
  expect(after.raw).toBe(before.raw);
  expect(after.segments.map((s) => s.clips[0].asset_id)).toEqual(
    before.segments.map((s) => s.clips[0].asset_id),
  );
  expect(after.segments[1].translation).toBe("");
  expect(generationRequests).toBe(1);
  await page.reload();
  await page.locator(".book-card").filter({ hasText: book.title }).click();
  await expect(page.locator(".translation p").first()).toHaveText(
    "风吹了过来。",
  );
  await page.getByRole("button", { name: "填写译文", exact: true }).click();
  await page.getByLabel("中文译文", { exact: true }).fill("我翻开了书。");
  await page.getByRole("button", { name: "保存修改" }).click();
  await expect(page.locator(".untranslated")).toHaveCount(0);
  expect(generationRequests).toBe(1);
  const progress = await (
    await request.get(`/api/books/${book.id}/progress`)
  ).json();
  expect(progress.chapter_id).toBe(created.id);
  const jobs: Job[] = await (
    await request.get(`/api/jobs?chapter=${created.id}`)
  ).json();
  expect(jobs.every((j) => j.kind === "audio")).toBe(true);
});

test("stale batch saves keep drafts visible and do not partially overwrite translations", async ({
  page,
  request,
}) => {
  const headers = { "X-Reader-Request": "1" };
  const b = await (
    await request.post("/api/books", {
      headers,
      data: { title: "手动版本冲突 " + Date.now(), author: "" },
    })
  ).json();
  const created = await (
    await request.post(`/api/books/${b.id}/chapters`, {
      headers,
      data: { title: "版本测试", raw: "一。\n二。" },
    })
  ).json();
  const c: Chapter = await (
    await request.get(`/api/chapters/${created.id}`)
  ).json();
  await page.goto("/");
  await page.locator(".book-card").filter({ hasText: b.title }).click();
  await page.getByRole("button", { name: "手动输入译文", exact: true }).click();
  await page
    .getByLabel("第 1 段中文译文", { exact: true })
    .fill("保留我的草稿");
  await page.getByLabel("第 2 段中文译文", { exact: true }).fill("旧版本编辑");
  await request.patch(`/api/segments/${c.segments[1].id}/translation`, {
    headers,
    data: { revision: 0, text: "另一页面已保存" },
  });
  await page.getByRole("button", { name: "保存已填写译文" }).click();
  await expect(page.getByRole("dialog").getByRole("alert")).toContainText(
    "版本已变化",
  );
  await expect(page.getByLabel("第 1 段中文译文", { exact: true })).toHaveValue(
    "保留我的草稿",
  );
  const saved: Chapter = await (
    await request.get(`/api/chapters/${created.id}`)
  ).json();
  expect(saved.segments[0].translation).toBe("");
  expect(saved.segments[1].translation).toBe("另一页面已保存");
  page.on("dialog", (dialog) => void dialog.accept());
  await page.getByRole("button", { name: "关闭译文编辑" }).click();
});
