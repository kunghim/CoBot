import { loadPresentation, getSlides, getSlideSize, getSlideTitle, type PresentationData } from '@office-kit/pptx';
import { renderSlideToSvg } from '@office-kit/pptx-preview';
import { validateWorkbookArchive } from './workbookArchive';
let presentation: PresentationData | undefined;
self.onmessage = async (event: MessageEvent<{ type: string; buffer?: ArrayBuffer; index?: number; request?: number }>) => {
  try {
    if (event.data.type === 'load' && event.data.buffer) {
      await validateWorkbookArchive(event.data.buffer, 'ppt/presentation.xml');
      presentation = await loadPresentation(new Uint8Array(event.data.buffer));
      const slides = getSlides(presentation);
      const size = getSlideSize(presentation);
      if (slides.length > 200 || !size || size.width <= 0 || size.height <= 0 || size.width > 100000000 || size.height > 100000000) throw new Error('Presentation limit');
      self.postMessage({ type: 'loaded', slides: slides.map(getSlideTitle), width: size.width / 9525, height: size.height / 9525 });
    } else if (event.data.type === 'slide' && presentation && Number.isInteger(event.data.index)) {
      const slide = getSlides(presentation)[event.data.index!];
      if (!slide) throw new Error('Invalid slide');
      const svg = renderSlideToSvg(presentation, slide);
      if (svg.length > 8 * 1024 * 1024) throw new Error('Presentation limit');
      self.postMessage({ type: 'slide', svg, request: event.data.request });
    }
  } catch (error) { self.postMessage({ type: 'error', limit: error instanceof Error && ['Presentation limit', 'Workbook limit'].includes(error.message) }); }
};
