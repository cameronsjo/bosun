# Assets

## Icon

The unops icon should be placed here as `icon.png`.

### Requirements

- **Size**: 512x512 pixels (required for Unraid CA)
- **Format**: PNG with transparency
- **Style**: Simple, recognizable at small sizes

### Image Generation Prompt

**Full prompt:**

```
Minimalist app icon for "unops" - a GitOps deployment tool for homelabs.

Design concept: A conductor's baton crossed with a Docker container, forming a subtle "U" shape.

Style:
- Flat design, no gradients
- Dark background (#1a1a2e or deep navy)
- Accent colors: Teal (#00d4aa) and orange (#ff6b35)
- Clean geometric shapes
- Visible at 64x64px

Elements to include:
- Conductor's baton (diagonal, elegant line with small tip)
- Docker whale silhouette OR container box shape
- Subtle git branch lines connecting them
- Musical staff lines as background texture (very subtle)

Do NOT include:
- Text or letters
- Complex details
- Realistic shading
- More than 3 colors

Output: 512x512 PNG with transparent background, suitable for Unraid Community Apps
```

**Simple prompt:**

```
Minimalist tech icon: conductor's baton pointing at a shipping container, teal and orange on dark navy, flat design, app icon style, 512x512
```

**Concept sketch:**

```
     ╲
      ╲  ← baton
       ╲
    ┌───╲───┐
    │   ╲   │  ← container
    │    ╲  │
    └───────┘
```

### To Add Icon

1. Generate icon using prompt above
2. Save as `assets/icon.png` (512x512 PNG)
3. Commit and push
4. Icon will be served from: `https://raw.githubusercontent.com/cameronsjo/unops/main/assets/icon.png`
