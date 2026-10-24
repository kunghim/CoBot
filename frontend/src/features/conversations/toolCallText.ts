// Some models (MiMo among them) write a tool call into their reply as text,
// in their own XML form, next to the real call or instead of it:
//
//   <tool_call><function=start_requirement><parameter=title>…</parameter></function></tool_call>
//
// That text is not meant to be read: it was never run and shows as a wall of
// markup. The stored reply is left as the model wrote it; it is only left out
// of what is shown.

const CLOSED = [/<tool_call>[\s\S]*?<\/tool_call>/g, /<function=[^>\n]*>[\s\S]*?<\/function>/g];
const OPENERS = ['<tool_call>', '<function='];

/** The reply without the tool calls written into it as text. A call still
 * being written (no closing tag yet, or the opening tag half typed) is left
 * out too, so it does not flash by while the reply streams. */
export function stripToolCallText(text: string): string {
  if (!text.includes('<')) return text;
  let out = text;
  for (const pattern of CLOSED) out = out.replace(pattern, '');
  for (const opener of OPENERS) {
    const at = out.indexOf(opener);
    if (at >= 0) out = out.slice(0, at);
    // The end of the text may be the first letters of an opener. Less than
    // six letters is too little to tell from ordinary text ("<table").
    for (let length = opener.length - 1; length >= 6; length--) {
      if (out.endsWith(opener.slice(0, length))) {
        out = out.slice(0, out.length - length);
        break;
      }
    }
  }
  if (out === text) return text;
  return out.replace(/\n{3,}/g, '\n\n').trimEnd();
}
