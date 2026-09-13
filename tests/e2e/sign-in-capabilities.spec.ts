import { expect, test } from "@playwright/test";

for (const scenario of [
  {
    name: "unconfigured",
    methods: [],
    github: false,
    password: false,
    openclaw: false,
    token: true,
  },
  {
    name: "password only",
    methods: ["password"],
    github: false,
    password: true,
    openclaw: false,
    token: false,
  },
  {
    name: "OpenClaw ID only",
    methods: ["openclaw"],
    github: false,
    password: false,
    openclaw: true,
    token: false,
  },
  {
    name: "older server",
    methods: null,
    github: true,
    password: false,
    openclaw: true,
    token: false,
  },
]) {
  test(`sign-in surfaces match ${scenario.name} capabilities`, async ({ page }) => {
    await page.route("**/app", async (route) => {
      const response = await route.fetch();
      const config = scenario.methods === null ? {} : { authMethods: scenario.methods };
      const body = (await response.text()).replace(
        "</head>",
        `<script>window.__CLICKCLACK_CONFIG__=${JSON.stringify(config)};</script></head>`,
      );
      await route.fulfill({ response, body });
    });
    await page.route("**/api/me", (route) =>
      route.fulfill({ status: 401, json: { error: "Sign in required" } }),
    );
    await page.goto("/app");
    const panel = page.getByRole("region", { name: "Sign in", exact: true });
    await expect(panel).toBeVisible();
    await expect(panel.getByRole("link", { name: "Continue with GitHub" })).toHaveCount(
      Number(scenario.github),
    );
    await expect(panel.getByLabel("Email or username")).toHaveCount(Number(scenario.password));
    await expect(panel.getByRole("link", { name: "Sign in with OpenClaw ID" })).toHaveCount(
      Number(scenario.openclaw),
    );
    await expect(panel.getByLabel("Sign-in token")).toBeVisible({ visible: scenario.token });
    if (scenario.name === "OpenClaw ID only") {
      await expect(
        panel.getByText("Sign in with OpenClaw ID to join the guest room.", { exact: true }),
      ).toBeVisible();
    }
  });
}
