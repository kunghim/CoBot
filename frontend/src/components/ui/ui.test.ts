import { createElement as h } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { Plus } from 'lucide-react';
import { Button, Field, IconButton, PanelToggle, SegmentedControl, Switch, TextInput } from './index';

const html = (node: ReturnType<typeof h>) => renderToStaticMarkup(node);

describe('ui components', () => {
  it('names icon-only buttons for screen readers and tooltips', () => {
    const markup = html(h(IconButton, { icon: Plus, label: '新建团队' }));
    expect(markup).toContain('aria-label="新建团队"');
    expect(markup).toContain('title="新建团队"');
  });

  it('disables a loading button and marks it busy', () => {
    const markup = html(h(Button, { loading: true }, '保存'));
    expect(markup).toContain('disabled=""');
    expect(markup).toContain('aria-busy="true"');
  });

  it('wires a field label, hint and error to its control', () => {
    const markup = html(h(Field, { label: '名称', error: '不能为空', children: h(TextInput, {}) }));
    const id = /<label for="([^"]+)"/.exec(markup)?.[1];
    expect(id).toBeTruthy();
    expect(markup).toContain(`id="${id}"`);
    expect(markup).toContain(`aria-describedby="${id}-error"`);
    expect(markup).toContain('aria-invalid="true"');
    expect(markup).toContain('role="alert"');
  });

  it('exposes switches and segmented controls with their state', () => {
    expect(html(h(Switch, { checked: true, onChange: () => undefined, label: '开' }))).toContain('role="switch" aria-checked="true"');
    const segmented = html(h(SegmentedControl, { label: '明暗', value: 'dark', onChange: () => undefined, options: [{ value: 'light', label: '浅色' }, { value: 'dark', label: '深色' }] }));
    expect(segmented).toContain('role="radiogroup"');
    expect(segmented.match(/aria-checked="true"/g)).toHaveLength(1);
    expect(segmented).toMatch(/tabindex="0"[^>]*>深色|aria-checked="true" tabindex="0"/);
  });

  it('labels a panel toggle by what pressing it does', () => {
    const open = html(h(PanelToggle, { side: 'left', open: true, onToggle: () => undefined, openLabel: '展开', closeLabel: '收起' }));
    const closed = html(h(PanelToggle, { side: 'left', open: false, onToggle: () => undefined, openLabel: '展开', closeLabel: '收起' }));
    expect(open).toContain('aria-label="收起"');
    expect(open).toContain('aria-expanded="true"');
    expect(closed).toContain('aria-label="展开"');
  });
});
