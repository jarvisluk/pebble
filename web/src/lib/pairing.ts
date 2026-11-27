export function stripLinkFragment(raw: string): string {
  const trimmed = raw.trim().replace(/[\r\n]+/g, '');
  const hash = trimmed.indexOf('#');
  return (hash >= 0 ? trimmed.slice(0, hash) : trimmed).trim();
}

export function maskedPairingLink(baseLink: string): string {
  const base = stripLinkFragment(baseLink);
  return base ? `${base}#token=••••••••` : '';
}

export function buildPairingLink(baseLink: string, token: string): string {
  const base = stripLinkFragment(baseLink);
  const tok = token.trim();
  if (!base || !tok) return '';
  return `${base}#token=${encodeURIComponent(tok)}`;
}
