# Fillable StewardMesh S mark

The SVG's green, teal, and blue interior faces are separate CSS targets. The reusable source can be transparent, while the checked-in application asset defaults to `#061827` for external image and favicon use.

Use it inline when you need CSS configuration. External `<img>` files cannot receive CSS custom properties from the host page.

```tsx
import SMark from "./stewardmesh-s-mark.svg?react";

// One fill for the whole interior
<SMark style={{ "--sm-fill": "#ffffff" } as React.CSSProperties} />

// Separate tier fills
<SMark
  style={{
    "--sm-fill-green": "#0b2238",
    "--sm-fill-teal": "#ffffff",
    "--sm-fill-blue": "transparent",
  } as React.CSSProperties}
/>
```

Or use a CSS class:

```css
.s-mark-on-dark {
  --sm-fill: #061827;
}

.s-mark-tiered {
  --sm-fill-green: color-mix(in srgb, #46cf51 12%, transparent);
  --sm-fill-teal: color-mix(in srgb, #16bfa7 12%, transparent);
  --sm-fill-blue: color-mix(in srgb, #1768ef 12%, transparent);
}
```

For a plain file used via `<img>`, set the desired default by editing `--sm-fill` at the top of the SVG: `transparent`, `#fff`, or `#061827`.

The checked-in detailed StewardMesh asset uses `#061827` so external image usage retains the intended dark interior. Browser tabs use the separate square `web/public/favicon.svg`, whose simpler, heavier geometry is designed for 16px rendering. The favicon URL is versioned in `web/index.html` when the asset changes so browsers refresh their cached icon.
