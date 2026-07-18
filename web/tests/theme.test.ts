import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

const parseTokens = (source: string) => Object.fromEntries(
  [...source.matchAll(/--([\w-]+):\s*(#[0-9a-f]{6})\s*;/gi)]
    .map((match) => [match[1], match[2]!.toLowerCase()]),
);

const luminance = (hex: string) => {
  const channels = [1, 3, 5]
    .map((offset) => Number.parseInt(hex.slice(offset, offset + 2), 16) / 255)
    .map((channel) => channel <= 0.04045 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4);
  return 0.2126 * channels[0]! + 0.7152 * channels[1]! + 0.0722 * channels[2]!;
};

const contrast = (a: string, b: string) => {
  const [bright, dark] = [luminance(a), luminance(b)].sort((left, right) => right - left);
  return (bright! + 0.05) / (dark! + 0.05);
};

describe('Anheyu brand_blue tokens', () => {
  it('pins the documented light and dark token snapshot', () => {
    const css = readFileSync(resolve(process.cwd(), 'src/styles/theme.css'), 'utf8');
    const [lightSource, darkSource = ''] = css.split('@media (prefers-color-scheme: dark)');
    const light = parseTokens(lightSource!);
    const dark = parseTokens(darkSource);
    expect({
      light: { primary:light['brand-primary'],onPrimary:light['brand-on-primary'],accent:light['brand-accent'],success:light['semantic-success'],warning:light['semantic-warning'],danger:light['semantic-danger'],info:light['semantic-info'] },
      dark: { primary:dark['brand-primary'],onPrimary:dark['brand-on-primary'],accent:dark['brand-accent'],success:dark['semantic-success'],warning:dark['semantic-warning'],danger:dark['semantic-danger'],info:dark['semantic-info'] },
    }).toEqual({
      light: { primary:'#163bf2',onPrimary:'#ffffff',accent:'#7a60d2',success:'#57bd6a',warning:'#c28b00',danger:'#d80020',info:'#3e86f6' },
      dark: { primary:'#f5b82a',onPrimary:'#1a1508',accent:'#a78bfa',success:'#3e9f50',warning:'#ffc93e',danger:'#ff3842',info:'#0084ff' },
    });
  });

  it('keeps branded controls and body text above WCAG AA contrast', () => {
    const css = readFileSync(resolve(process.cwd(), 'src/styles/theme.css'), 'utf8');
    const [lightSource, darkSource = ''] = css.split('@media (prefers-color-scheme: dark)');
    const light = parseTokens(lightSource!);
    const dark = parseTokens(darkSource);
    for (const [foreground, background] of [
      [light['brand-on-primary'], light['brand-primary']],
      [light['text-primary'], light['surface-panel']],
      [dark['brand-on-primary'], dark['brand-primary']],
      [dark['text-primary'], dark['surface-panel']],
    ] as const) {
      expect(contrast(foreground!, background!)).toBeGreaterThanOrEqual(4.5);
    }
    expect(css).toContain('--focus-ring: var(--brand-primary)');
  });
});
