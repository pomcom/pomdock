import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

function luminance(hex: string) {
  let color = hex.replace('#', '');
  if (color.length === 3) color = color.split('').map(c => c + c).join('');
  const rgb = [0, 2, 4].map(offset => parseInt(color.slice(offset, offset + 2), 16) / 255).map(value => value <= .04045 ? value / 12.92 : ((value + .055) / 1.055) ** 2.4);
  return rgb[0] * .2126 + rgb[1] * .7152 + rgb[2] * .0722;
}
function contrast(a: string, b: string) { const values = [luminance(a), luminance(b)].sort((x,y) => y-x); return (values[0]+.05)/(values[1]+.05); }
const css = readFileSync(new URL('../app.css', import.meta.url), 'utf8');
for (const [name, block] of [['dark', css.match(/:root \{([^}]+)\}/s)?.[1]], ['light', css.match(/:root\[data-theme="light"\] \{([^}]+)\}/s)?.[1]]] as const) {
  test(`${name} theme semantic text, focus and essential borders meet contrast requirements`, () => {
    const tokens = Object.fromEntries([...block!.matchAll(/--pd-([\w-]+):\s*(#[\da-f]+)/g)].map(match => [match[1], match[2]]));
    for (const surface of ['bg','sidebar','surface','elevated','terminal','hover','selected']) {
      for (const text of ['text','text-muted']) assert(contrast(tokens[text],tokens[surface]) >= 4.5, `${text} on ${surface}`);
      assert(contrast(tokens.accent,tokens[surface]) >= 3, `focus on ${surface}`);
    }
    for (const surface of ['bg','surface','elevated','terminal']) assert(contrast(tokens['control-border'],tokens[surface]) >= 3, `control border on ${surface}`);
    assert(contrast(tokens['on-accent'],tokens.accent) >= 4.5, 'filled action text');
    for (const status of ['success','warning','danger']) assert(contrast(tokens[status],tokens.surface) >= 4.5, `${status} text`);
  });
}
