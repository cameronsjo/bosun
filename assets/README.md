# Assets

## Icon

The bosun icon should be placed here as `icon.png`.

### Requirements

- **Size**: 512x512 pixels (required for Unraid CA)
- **Format**: PNG with transparency
- **Style**: Simple, recognizable at small sizes

### Image Generation Prompt

**Full prompt:**

```
Minimalist app icon for "bosun" - a GitOps deployment tool for homelabs with a nautical theme.

Design concept: A bosun's whistle (boatswain's call) combined with a shipping container, suggesting command and deployment.

Style:
- Flat design, no gradients
- Dark background (#1a1a2e or deep navy)
- Accent colors: Teal (#00d4aa) and brass/gold (#d4a574)
- Clean geometric shapes
- Visible at 64x64px

Elements to include:
- Bosun's whistle silhouette (the distinctive curved pipe shape)
- Shipping container or Docker whale hint
- Subtle nautical rope or anchor accent
- Wave pattern as subtle background texture

Do NOT include:
- Text or letters
- Complex details
- Realistic shading
- More than 3 colors

Output: 512x512 PNG with transparent background, suitable for Unraid Community Apps
```

**Simple prompt:**

```
Minimalist nautical tech icon: bosun's whistle with shipping container, teal and brass gold on dark navy, flat design, app icon style, 512x512
```

**Concept sketch:**

```
      ╭──╮
     ╱    ╲  ← whistle pipe
    │      │
    ╰──┬───╯
    ┌──┴───┐
    │ ▣ ▣  │  ← container
    └──────┘
```

### To Add Icon

1. Generate icon using prompt above
2. Save as `assets/icon.png` (512x512 PNG)
3. Commit and push
4. Icon will be served from: `https://raw.githubusercontent.com/cameronsjo/bosun/main/assets/icon.png`
