const hostedProductHosts = new Set(["clickclack.chat", "www.clickclack.chat"]);

export function productAppURLForHost(hostname: string): string {
  return hostedProductHosts.has(hostname.toLowerCase()) ? "https://app.clickclack.chat" : "/app";
}

export function isLoopbackHostname(hostname: string): boolean {
  const normalized = hostname
    .toLowerCase()
    .replace(/^\[|\]$/g, "")
    .replace(/\.$/, "");
  if (
    normalized === "localhost" ||
    normalized.endsWith(".localhost") ||
    normalized === "::1" ||
    normalized === "::ffff:127.0.0.1"
  ) {
    return true;
  }
  const octets = normalized.split(".");
  return (
    octets.length === 4 &&
    octets.every((octet) => /^\d{1,3}$/.test(octet) && Number(octet) <= 255) &&
    Number(octets[0]) === 127
  );
}
