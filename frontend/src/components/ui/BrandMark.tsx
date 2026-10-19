import mark from '../../assets/branding/brandmark-128.png';

/** The fixed metallic brand mark. Its light plate keeps contrast in every theme. */
export function BrandMark({ size = 36, label }: { size?: number; label?: string }) {
  return <img src={mark} alt={label ?? ''} width={size} height={size} draggable={false} className="shrink-0 object-contain" />;
}
