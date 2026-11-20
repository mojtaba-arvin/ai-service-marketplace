import { expect, test, type Page, type APIRequestContext } from "@playwright/test";

const otpCaptureURL = process.env.E2E_OTP_CAPTURE_URL ?? "http://127.0.0.1:8081/__e2e/otp";

async function capturedOTP(request: APIRequestContext, phone: string): Promise<string> {
  let code = "";
  await expect.poll(async () => {
    const response = await request.get(`${otpCaptureURL}?phone=${encodeURIComponent(phone)}`);
    if (!response.ok()) return "";
    const payload = (await response.json()) as { code?: string };
    code = payload.code ?? "";
    return code;
  }, { timeout: 10_000, intervals: [100, 250, 500] }).toMatch(/^\d{6}$/);
  return code;
}

async function signIn(page: Page, request: APIRequestContext, phone: string): Promise<void> {
  await page.goto("/");
  await page.getByLabel("Phone number").fill(phone);
  await page.getByRole("button", { name: "Send verification code" }).click();
  const code = await capturedOTP(request, phone);
  await page.getByLabel("Verification code").fill(code);
  const verificationResponsePromise = page.waitForResponse((response) =>
    response.url().endsWith("/v1/auth/otp/verify"),
  );
  await page.getByRole("button", { name: "Verify and continue" }).click();
  const verificationResponse = await verificationResponsePromise;
  const verificationBody = await verificationResponse.text();
  expect(verificationResponse.status(), `OTP verification response: ${verificationBody}`).toBe(200);
  try {
    await expect(page.getByRole("heading", { name: "Good service starts with trust." })).toBeVisible();
  } catch {
    const body = await page.locator("body").innerText();
    const sessionStatus = await page.evaluate(async () => {
      const response = await fetch("/v1/auth/me", { credentials: "same-origin" });
      return { status: response.status, body: await response.text() };
    });
    throw new Error(`Session did not become active after OTP verification. OTP response=${verificationBody}; auth/me=${JSON.stringify(sessionStatus)}; page=${body}`);
  }
}

async function signOut(page: Page): Promise<void> {
  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page.getByLabel("Phone number")).toBeVisible();
}

test("customer booking → operator verification/dispatch → professional acceptance → customer status; cookie security", async ({
  page,
  request,
}) => {
  const baseURL = test.info().project.use.baseURL as string;
  const customerPhone = "+15550000001";
  const operatorPhone = "+15550000003";
  const professionalPhone = "+15550000002";

  // Authenticate through the real OTP challenge and session persistence code.
  await signIn(page, request, customerPhone);

  const cookies = await page.context().cookies(baseURL);
  const accessCookie = cookies.find((cookie) => cookie.name === "marketplace_access");
  const refreshCookie = cookies.find((cookie) => cookie.name === "marketplace_refresh");
  const csrfCookie = cookies.find((cookie) => cookie.name === "marketplace_csrf");
  expect(accessCookie).toMatchObject({ httpOnly: true, secure: true, sameSite: "Lax" });
  expect(refreshCookie).toMatchObject({ httpOnly: true, secure: true, sameSite: "Lax" });
  expect(csrfCookie).toMatchObject({ httpOnly: false, secure: true, sameSite: "Lax" });
  expect(await page.evaluate(() => document.cookie)).not.toContain("marketplace_access");
  expect(await page.evaluate(() => document.cookie)).not.toContain("marketplace_refresh");

  // Unsafe cookie-authenticated requests must fail without the CSRF header,
  // and must also fail when the Origin does not match the ingress host.
  const missingCSRF = await page.evaluate(async () => {
    const response = await fetch("/v1/auth/session/refresh", {
      method: "POST",
      headers: { "X-Session-Transport": "cookie" },
    });
    return response.status;
  });
  expect(missingCSRF).toBe(403);

  const csrfToken = await page.evaluate(() => {
    const item = document.cookie.split(";").map((part) => part.trim())
      .find((part) => part.startsWith("marketplace_csrf="));
    return item ? decodeURIComponent(item.slice("marketplace_csrf=".length)) : "";
  });
  expect(csrfToken).not.toBe("");
  const cookieHeader = cookies.map((cookie) => `${cookie.name}=${cookie.value}`).join("; ");
  const crossOrigin = await page.context().request.post(`${baseURL}/v1/auth/session/refresh`, {
    headers: {
      Cookie: cookieHeader,
      Origin: "https://attacker.invalid",
      "X-CSRF-Token": csrfToken,
      "X-Session-Transport": "cookie",
    },
  });
  expect(crossOrigin.status()).toBe(403);

  const validRefresh = await page.evaluate(async () => {
    const item = document.cookie.split(";").map((part) => part.trim())
      .find((part) => part.startsWith("marketplace_csrf="));
    const token = item ? decodeURIComponent(item.slice("marketplace_csrf=".length)) : "";
    const response = await fetch("/v1/auth/session/refresh", {
      method: "POST",
      headers: { "X-Session-Transport": "cookie", "X-CSRF-Token": token },
    });
    return response.status;
  });
  expect(validRefresh).toBe(200);

  // Force access-token expiry. The web client must rotate the refresh cookie
  // and recover the authenticated page without a new OTP challenge.
  await page.waitForTimeout(5_000);
  await page.reload();
  await expect(page.getByRole("heading", { name: "Good service starts with trust." })).toBeVisible();

  await expect(page.locator("#service")).toBeVisible();
  const scheduled = new Date(Date.now() + 3 * 60 * 60 * 1000);
  const pad = (value: number) => String(value).padStart(2, "0");
  const scheduleValue = `${scheduled.getUTCFullYear()}-${pad(scheduled.getUTCMonth() + 1)}-${pad(scheduled.getUTCDate())}T${pad(scheduled.getUTCHours())}:${pad(scheduled.getUTCMinutes())}`;
  await page.locator("#schedule").fill(scheduleValue);
  await page.getByRole("button", { name: "Create service request" }).click();
  await expect(page.getByText("Your service request has been created.")).toBeVisible();
  await expect(page.locator(".order-card .status-requested").first()).toBeVisible();

  // The operator verifies the seeded professional before dispatch.
  await signOut(page);
  await signIn(page, request, operatorPhone);
  await page.getByRole("button", { name: "Operations" }).click();
  await expect(page.getByText("E2E Professional", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Verify and activate" }).click();
  await expect(page.getByText("Professional profile and skill verified; provider activated.")).toBeVisible();
  await page.getByRole("button", { name: "Dispatch to eligible professionals" }).click();
  await expect(page.getByText("Offers sent to eligible professionals.")).toBeVisible();

  // The professional accepts the real persisted dispatch offer.
  await signOut(page);
  await signIn(page, request, professionalPhone);
  await page.getByRole("button", { name: "Professional" }).click();
  await expect(page.getByRole("heading", { name: "Offers for you" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Accept" }).first()).toBeVisible();
  await page.getByRole("button", { name: "Accept" }).first().click();
  await expect(page.getByText("Offer accepted. The order is now assigned to you.")).toBeVisible();

  // A fresh customer session must see the persisted assigned/accepted status.
  await signOut(page);
  await signIn(page, request, customerPhone);
  await expect(page.locator(".order-card .status-accepted").first()).toBeVisible();

  await signOut(page);
  const remainingCookies = await page.context().cookies(baseURL);
  expect(remainingCookies.some((cookie) => cookie.name === "marketplace_access")).toBe(false);
  expect(remainingCookies.some((cookie) => cookie.name === "marketplace_refresh")).toBe(false);
  const unauthenticatedStatus = await page.evaluate(async () => {
    const response = await fetch("/v1/auth/me", { credentials: "same-origin" });
    return response.status;
  });
  expect(unauthenticatedStatus).toBe(401);
});
