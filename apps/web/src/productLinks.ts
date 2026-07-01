const hostedProductHosts = new Set(["clickclack.chat", "www.clickclack.chat"]);

export function productAppURLForHost(hostname: string): string {
  return hostedProductHosts.has(hostname.toLowerCase()) ? "https://app.clickclack.chat" : "/app";
}
