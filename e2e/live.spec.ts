import { expect, test, type WebSocketRoute } from "@playwright/test";
import { expectNoSidewaysScroll } from "./layout";

// Labeled transport fixtures exercise update/reconnect rendering. Real account
// isolation, native records and chat are covered separately by local acceptance.
for (const width of [1440, 390]) {
  test(`live refresh retains feed and drafts at ${width}px`, async ({
    page,
  }) => {
    test.setTimeout(60000);
    await page.setViewportSize({ width, height: 900 });
    let revision = 1,
      slow = false,
      failed = false,
      offline = false,
      reads = 0;
    const sockets: WebSocketRoute[] = [];
    await page.routeWebSocket("**/api/v1/realtime*", (ws) => {
      sockets.push(ws);
      if (offline) ws.close({ code: 1013, reason: "Fixture outage" });
    });
    await page.route("**/api/v1/auth/session", (route) =>
      route.fulfill({
        json: {
          user: {
            subject: "live-fixture",
            name: "Live fixture",
            email: "fixture@example.invalid",
            verified: true,
          },
        },
      }),
    );
    await page.route("**/api/v1/connections", (route) =>
      route.fulfill({
        json: {
          items: [
            {
              id: "fixture",
              network: "bluesky",
              hosted: true,
              subject: "did:plc:fixture",
              server: "https://fixture.invalid",
              handle: "fixture.invalid",
              capabilities: ["feed", "reply", "like", "publish", "zing-chat"],
            },
          ],
        },
      }),
    );
    await page.route("**/api/v1/hosting", (route) =>
      route.fulfill({ json: { identities: [], networks: [] } }),
    );
    await page.route("**/api/v1/feed?*", async (route) => {
      reads++;
      if (slow) await new Promise((resolve) => setTimeout(resolve, 1400));
      if (failed) {
        await route.fulfill({
          status: 503,
          json: { error: "Fixture provider unavailable" },
        });
        return;
      }
      await route.fulfill({
        json: {
          items: [
            {
              id: "fixture-post",
              cid: "fixture-cid",
              connectionId: "fixture",
              network: "bluesky",
              text: "Labeled live fixture post",
              author: {
                id: "did:plc:fixture",
                name: "Fixture author",
                handle: "fixture.invalid",
                avatar: "",
                bio: "",
                followers: 0,
                following: 0,
              },
              createdAt: "2026-11-05T09:00:00Z",
              url: "",
              images: [],
              likes: revision,
              replies: 0,
              reposts: 0,
              liked: false,
              reposted: false,
              visibility: "public",
            },
          ],
          warnings: [],
          cursor: "",
        },
      });
    });
    await page.goto("/");
    const post = page
      .locator("article")
      .filter({ hasText: "Labeled live fixture post" });
    await expect(post).toBeVisible();
    await expectNoSidewaysScroll(page);
    await post.evaluate((el) => {
      (window as unknown as { __post: Element }).__post = el;
    });
    const before = await post.boundingBox();
    const like = post.getByRole("button", { name: "Like", exact: true });
    await like.focus();
    const color = await like.evaluate((el) => getComputedStyle(el).color);
    const update = () =>
      sockets.at(-1)?.send(JSON.stringify({ kind: "hosted-feed-updated" }));
    for (revision = 2; revision <= 4; revision++) {
      update();
      await expect(like).toContainText(String(revision));
      await expect(like).toBeFocused();
      expect(
        await post.evaluate(
          (el) => el === (window as unknown as { __post: Element }).__post,
        ),
      ).toBe(true);
      expect(await post.boundingBox()).toEqual(before);
      expect(await like.evaluate((el) => getComputedStyle(el).color)).toBe(
        color,
      );
      await expect(
        page.getByText("Loading feed…", { exact: true }),
      ).toHaveCount(0);
    }
    slow = true;
    revision = 5;
    update();
    await page.waitForTimeout(300);
    await expect(like).toContainText("4");
    await expect(post).toBeVisible();
    await expect(like).toBeFocused();
    await expect(like).toContainText("5");
    slow = false;
    failed = true;
    update();
    await expect(page.getByRole("alert")).toContainText(
      "Fixture provider unavailable",
    );
    await expect(like).toContainText("5");
    failed = false;
    revision = 6;
    update();
    await expect(like).toContainText("6");
    await expect(page.getByRole("alert")).toHaveCount(0);
    sockets.at(-1)?.close({ code: 1013 });
    revision = 7;
    await expect(like).toContainText("7");
    offline = true;
    sockets.at(-1)?.close({ code: 1013 });
    await page.waitForTimeout(4200);
    await expect(like).toContainText("7");
    expect(
      await post.evaluate(
        (el) => el === (window as unknown as { __post: Element }).__post,
      ),
    ).toBe(true);
    offline = false;
    revision = 8;
    await page.evaluate(() => window.dispatchEvent(new Event("online")));
    await expect(like).toContainText("8");
    await page
      .getByRole("button", { name: "Pause live updates", exact: true })
      .click();
    const pausedReads = reads;
    revision = 9;
    await page.waitForTimeout(1200);
    expect(reads).toBe(pausedReads);
    await expect(like).toContainText("8");
    await page
      .getByRole("button", { name: "Resume live updates", exact: true })
      .click();
    await expect(like).toContainText("9");
    await page
      .getByRole("button", { name: "Create a post", exact: true })
      .first()
      .click();
    const draft = page.getByRole("textbox", { name: "Post text", exact: true });
    await draft.fill("Retained unsent draft");
    await draft.focus();
    await page.evaluate(() => window.scrollTo(0, 150));
    const scroll = await page.evaluate(() => scrollY);
    for (let i = 0; i < 3; i++) {
      update();
      await page.waitForTimeout(150);
      await expect(draft).toHaveValue("Retained unsent draft");
      await expect(draft).toBeFocused();
      expect(await page.evaluate(() => scrollY)).toBe(scroll);
    }
    await expectNoSidewaysScroll(page);
  });
}
