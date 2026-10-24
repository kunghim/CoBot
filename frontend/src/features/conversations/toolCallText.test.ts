import { describe, expect, it } from 'vitest';
import { stripToolCallText } from './toolCallText';

describe('stripToolCallText', () => {
  it('leaves an ordinary reply alone', () => {
    const text = '修改了 <div> 的样式，见 a < b 的比较。\n\n```html\n<table></table>\n```';
    expect(stripToolCallText(text)).toBe(text);
  });

  it('takes a written tool call out and keeps what is said around it', () => {
    const text =
      '我会提交需求。<tool_call><function=_x__mcp__cobot__start_requirement><parameter=title>标题</parameter><parameter=document>## 目标\n内容</parameter></function></tool_call>\n\n已提交，等你确认。';
    expect(stripToolCallText(text)).toBe('我会提交需求。\n\n已提交，等你确认。');
  });

  it('takes out a call with no wrapper, and several calls', () => {
    expect(stripToolCallText('a<function=x><parameter=p>1</parameter></function>b<tool_call>c</tool_call>d')).toBe('abd');
  });

  it('hides a call that is still being written', () => {
    expect(stripToolCallText('提交中<tool_call><function=x><parameter=document>## 目')).toBe('提交中');
    expect(stripToolCallText('提交中<tool_c')).toBe('提交中');
    expect(stripToolCallText('提交中<tool_call')).toBe('提交中');
    expect(stripToolCallText('提交中<function')).toBe('提交中');
  });

  it('is empty when the reply was only a call', () => {
    expect(stripToolCallText('<tool_call><function=x></function></tool_call>')).toBe('');
  });
});
