import type { HarnessInfo, SettingsData } from '../types';

export function installedHarnesses(settings: SettingsData | null | undefined): HarnessInfo[] {
  return (settings?.harnesses || []).filter((h) => h.installed === true);
}

export function harnessSelectionLabel(
  id: string,
  settings: SettingsData | null | undefined,
  notInstalledText: string
): string {
  if (!id) return notInstalledText;
  const h = settings?.harnesses?.find((item) => item.id === id);
  const label = h?.label || id;
  return h?.installed === true ? label : `${label} (${notInstalledText})`;
}
