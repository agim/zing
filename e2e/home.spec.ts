import { expect, test } from "@playwright/test";
import { expectNoSidewaysScroll } from "./layout";

// The application replaces the scaffold's greeting with real authentication.
// Retain the hydration/error/layout checks and assert the product's routes.
test("home hydrates, loads session, and navigates to the privacy policy", async ({
  page,
}) => {
  const errors: string[] = [];
  await page.addInitScript(() => {
    (window as unknown as { __errors: string[] }).__errors = [];
    window.addEventListener("error", (event) =>
      (window as unknown as { __errors: string[] }).__errors.push(
        String(event.error?.message ?? event.message),
      ),
    );
  });
  page.on("console", (m) => {
    if (m.type() === "error") errors.push(m.text());
  });
  await page.goto("/");
  await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Welcome back." }),
  ).toBeVisible();
  await page.getByRole("link", { name: "privacy policy" }).click();
  await expect(
    page.getByRole("heading", { name: "Privacy", exact: true }),
  ).toBeVisible();
  expect(errors).toEqual([]);
  expect(
    await page.evaluate(
      () => (window as unknown as { __errors: string[] }).__errors || [],
    ),
  ).toEqual([]);
});
test("home fits a phone", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/");
  await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
  await expectNoSidewaysScroll(page);
});
test("privacy ships without the client runtime", async ({ page }) => {
  const scripts: string[] = [];
  page.on("request", (r) => {
    if (r.resourceType() === "script") scripts.push(r.url());
  });
  await page.goto("/privacy");
  await expect(
    page.getByRole("heading", { name: "Privacy", exact: true }),
  ).toBeVisible();
  await expect(
    page.locator('script:not([type="application/ld+json"])'),
  ).toHaveCount(0);
  expect(scripts).toEqual([]);
});
test("registration requires verification and never invents connected accounts", async ({
  page,
}) => {
  await page.goto("/register");
  await page.getByLabel("Name", { exact: true }).fill("Browser user");
  await page
    .getByLabel("Email", { exact: true })
    .fill(
      `browser-${Date.now()}-${Math.random().toString(36).slice(2)}@zing.example`,
    );
  await page
    .getByLabel("Password", { exact: true })
    .fill("browser-test-password-123");
  await page
    .getByRole("button", { name: "Create account", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "Check your inbox." }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Resend verification email" }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Connect accounts", exact: true }),
  ).toHaveCount(0);
});
test("failed sign-in retains email and displays a real error", async ({
  page,
}) => {
  await page.goto("/login");
  await page.getByLabel("Email", { exact: true }).fill("missing@zing.example");
  await page
    .getByLabel("Password", { exact: true })
    .fill("not-a-real-password");
  await page.getByRole("button", { name: "Continue", exact: true }).click();
  await expect(page.getByRole("alert")).toBeVisible();
  await expect(page.getByLabel("Email", { exact: true })).toHaveValue(
    "missing@zing.example",
  );
});

// Seed only through real registration + the test mailbox, never a product bypass.
test("verified account sees honest empty states and can switch themes on mobile", async ({
  page,
}) => {
  const { execFileSync } = await import("node:child_process");
  const email = `verified-${Date.now()}-${Math.random().toString(36).slice(2)}@zing.example`;
  const registered = await page.request.post("/api/v1/auth/register", {
    data: {
      email,
      password: "browser-verified-password-123",
      name: "Verified user",
    },
  });
  expect(registered.status()).toBe(201);
  try {
    // The browser runner receives the dedicated test DATABASE_URL from lidza test.
    // The generated email is fixture data and its token is never printed.
    const dsn =
      process.env.DATABASE_URL ||
      "postgres:///zing_test?host=/var/run/postgresql";
    const sql = `SELECT coalesce(text,'') || coalesce(html,'') FROM mail_message WHERE recipient='${email}' ORDER BY created_at DESC LIMIT 1`;
    const content = execFileSync("psql", [dsn, "-Atc", sql], {
      encoding: "utf8",
    });
    const token = content.match(/[?&]token=([A-Za-z0-9_-]+)/)?.[1];
    expect(
      token,
      "verification link was written to the real test outbox",
    ).toBeTruthy();
    const verified = await page.request.post("/api/v1/auth/verify", {
      data: { token },
    });
    expect(verified.status()).toBe(204);
    await page.goto("/");
    await expect(
      page.getByRole("heading", { name: "Your world starts here." }),
    ).toBeVisible();
    await page
      .getByRole("button", { name: "Settings", exact: true })
      .first()
      .click();
    await expect(
      page.getByRole("heading", { name: "Connected accounts", exact: true }),
    ).toBeVisible();
    await page.getByLabel("Theme", { exact: true }).selectOption("dark");
    await expect(page.locator("body")).toHaveAttribute("data-theme", "dark");
    await page.reload();
    await expect(page.locator("body")).toHaveAttribute("data-theme", "dark");
    await page
      .getByRole("button", { name: "Create a post", exact: true })
      .click();
    await page
      .getByRole("textbox", { name: "Post text", exact: true })
      .fill("An idea before connecting any accounts");
    await page.getByRole("button", { name: "Save draft", exact: true }).click();
    await expect(page).toHaveURL(/#Publishing$/);
    await expect(
      page.getByRole("heading", { name: "Ideas, in motion.", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText("An idea before connecting any accounts", { exact: true }),
    ).toBeVisible();
    await page.reload();
    await expect(
      page.getByText("An idea before connecting any accounts", { exact: true }),
    ).toBeVisible();
    await page
      .getByRole("button", { name: "Continue writing", exact: true })
      .click();
    await expect(
      page.getByRole("textbox", { name: "Post text", exact: true }),
    ).toHaveValue("An idea before connecting any accounts");
    await expect(
      page.getByRole("button", { name: "Publish", exact: true }),
    ).toBeDisabled();
    await page.setViewportSize({ width: 390, height: 844 });
    await expectNoSidewaysScroll(page);
    await page.getByRole("button", { name: "Messages", exact: true }).click();
    await expect(
      page.getByRole("heading", { name: "Your inbox is quiet." }),
    ).toBeVisible();
    await expectNoSidewaysScroll(page);
  } finally {
    // A verified fixture enrolls on newly configured networks. Delete through
    // the real lifecycle so later test-database copies contain no orphan work.
    const deleted = await page.request.post("/api/v1/auth/delete", {
      data: { password: "browser-verified-password-123" },
      headers: {
        Origin: new URL(registered.url()).origin,
        "Sec-Fetch-Site": "same-origin",
      },
    });
    expect(deleted.status(), "browser fixture account was removed").toBe(204);
  }
});

test("first-run page uses installation status and does not expose credentials", async ({
  page,
}) => {
  await page.goto("/setup");
  await expect(
    page.getByRole("heading", { name: "Your own little world." }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "1. Create your operator account" }),
  ).toBeVisible();
  await expect(page.getByLabel("Password", { exact: true })).toHaveAttribute(
    "type",
    "password",
  );
  await expect(
    page.getByText(
      "Settings and credentials are saved encrypted in PostgreSQL.",
      { exact: false },
    ),
  ).toBeVisible();
  await page.setViewportSize({ width: 390, height: 844 });
  await expectNoSidewaysScroll(page);
});
