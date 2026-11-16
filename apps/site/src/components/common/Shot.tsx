// Shot is a screenshot of the app (docs/assets/<name>.png): AVIF and WebP in two widths before the PNG, which keeps its size as it loads. sizes is the picture's width in the layout, so that the browser picks the width it needs.
import site from "virtual:spruce/site";
import type { ShotName } from "../../lib/site";

export function Shot({ name, alt, sizes }: { name: ShotName; alt: string; sizes: string }) {
  const shot = site.shots[name];
  return (
    <picture>
      {shot.sources.map((s) => (
        <source key={s.type} type={s.type} srcSet={s.srcset} sizes={sizes} />
      ))}
      <img src={shot.src} width={shot.w} height={shot.h} alt={alt} loading="lazy" decoding="async" />
    </picture>
  );
}
