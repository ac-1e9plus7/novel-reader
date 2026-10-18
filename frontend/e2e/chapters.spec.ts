import { test, expect } from "@playwright/test";
import type { Chapter, Job } from "../src/types";

test("reorder and delete chapters, including the currently open and final chapter", async ({
  page,
  request,
}) => {
  const headers = { "X-Reader-Request": "1" };
  const book = await (
    await request.post("/api/books", {
      headers,
      data: { title: "章节管理 " + Date.now(), author: "" },
    })
  ).json();
  for (const title of ["第一章", "第二章", "第三章"])
    await request.post(`/api/books/${book.id}/chapters`, {
      headers,
      data: { title, raw: `${title}の本文。` },
    });
  await page.goto("/");
  await page.locator(".book-card").filter({ hasText: book.title }).click();
  const openDirectory = async () => {
    if (
      test.info().project.name === "mobile" &&
      !(await page.locator(".sidebar").isVisible())
    )
      await page.getByRole("button", { name: "章节目录", exact: true }).click();
  };
  await openDirectory();
  await page
    .getByRole("button", { name: "上移章节：第三章", exact: true })
    .click();
  await expect
    .poll(async () =>
      (await (await request.get(`/api/books/${book.id}/chapters`)).json()).map(
        (c: Chapter) => c.title,
      ),
    )
    .toEqual(["第一章", "第三章", "第二章"]);
  await page
    .getByRole("button", { name: "下移章节：第一章", exact: true })
    .click();
  await expect
    .poll(async () =>
      (await (await request.get(`/api/books/${book.id}/chapters`)).json()).map(
        (c: Chapter) => c.title,
      ),
    )
    .toEqual(["第三章", "第一章", "第二章"]);
  await page.reload();
  await page.locator(".book-card").filter({ hasText: book.title }).click();
  await openDirectory();
  await expect(page.locator(".chapter-link").first()).toContainText("第三章");
  page.on("dialog", (d) => void d.accept());
  await page
    .getByRole("button", { name: "删除章节：第一章", exact: true })
    .click();
  await expect(page.locator(".chapter-heading h1")).toHaveText("第二章");
  await openDirectory();
  await page
    .getByRole("button", { name: "删除章节：第三章", exact: true })
    .click();
  await expect(page.locator(".chapter-link")).toHaveCount(1);
  await page
    .getByRole("button", { name: "删除章节：第二章", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "书已备好，等故事落笔" }),
  ).toBeVisible();
  await expect(page.locator(".player")).toHaveCount(0);
  expect(
    await (await request.get(`/api/books/${book.id}/chapters`)).json(),
  ).toEqual([]);
});

test("translation is explicitly per paragraph in every reading mode", async ({
  page,
  request,
}) => {
  const headers = { "X-Reader-Request": "1" };
  const b = await (
    await request.post("/api/books", {
      headers,
      data: { title: "单段翻译 " + Date.now(), author: "" },
    })
  ).json();
  const created = await (
    await request.post(`/api/books/${b.id}/chapters`, {
      headers,
      data: { title: "逐段测试", raw: "一日目。\n二日目。\n三日目。" },
    })
  ).json();
  const c: Chapter = await (
    await request.get(`/api/chapters/${created.id}`)
  ).json();
  for (const segments of [[], c.segments.map((s) => s.id)]) {
    const response = await request.post(
      `/api/chapters/${created.id}/translation-jobs`,
      { headers, data: { revision: 1, segments, force: false } },
    );
    expect(response.status()).toBe(400);
  }
  await page.goto("/");
  await page.locator(".book-card").filter({ hasText: b.title }).click();
  await expect(
    page.getByRole("button", { name: "生成翻译", exact: true }),
  ).toHaveCount(0);
  await page.getByRole("button", { name: "翻译第2段", exact: true }).click();
  await expect(page.locator(".translation p").nth(1)).toContainText("窗外的风");
  await expect(page.locator(".untranslated")).toHaveCount(2);
  await page.getByRole("button", { name: "日文", exact: true }).click();
  await page.getByRole("button", { name: "翻译第1段", exact: true }).click();
  await expect
    .poll(
      async () =>
        (await (await request.get(`/api/chapters/${created.id}`)).json())
          .segments[0].translation,
    )
    .not.toBe("");
  const jobs: Job[] = await (
    await request.get(`/api/jobs?chapter=${created.id}`)
  ).json();
  expect(jobs).toHaveLength(2);
  expect(jobs.every((j) => j.total === 1)).toBe(true);
  const after: Chapter = await (
    await request.get(`/api/chapters/${created.id}`)
  ).json();
  expect(after.segments[2].translation).toBe("");
});
