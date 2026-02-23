# Bosun Transparent

Chibi-proportioned nautical officer, optimized for transparent background compositing over dark UI surfaces.

## Palette

- **Background**: Plain solid black (#000000) -- removed in post-processing
- **Peacoat**: Deep navy blue (#1B2A4A)
- **Brass accents**: Warm gold (#C49B2F) for buttons and whistle details
- **Whistle glow**: Warm amber (#FFAA33) with soft bloom
- **Skin**: Weathered tan (#D4A574)
- **Eyes**: Amber-brown (#B8763C)
- **Rope**: Natural hemp (#C9B38C)
- **Rim lighting**: Amber (#FFB347) from left, cool cyan (#5EC4D4) from right

## Medium

Clean stylized 3D render. Smooth surfaces with material differentiation: matte wool texture on peacoat, metallic sheen on brass buttons and whistle, soft subsurface scattering on skin. Not photorealistic -- closer to Pixar short or high-quality game character model. Strong rim lighting for silhouette clarity against black background.

## Mood

Calm competence. Steady hands, clear eyes, ready for whatever the ship throws at him.

## Imperfections

Minimal -- clean edges required for background removal. Soft amber bloom on the bosun's whistle only. No atmospheric haze, no smoke, no fog near subject edges.

## Text Treatment

NO text in generated images. All text handled by the compositing target.

## Composition

- **Subject-focused**: Bosun character with minimal props (whistle, rope), no environment
- Square aspect ratio (1:1)
- Subject fills ~60-70% of the frame vertically
- Plain solid black background with NO gradients, NO environment, NO floor, NO reflections
- Props kept close to subject -- rope over shoulder, whistle at chest
- Clean silhouette edges -- dual rim lighting (amber left, cyan right) for separation
- No ground plane, no shadows cast on background

## Post-Processing

Images generated with solid black backgrounds, then processed to transparent PNG using rembg (see transparent-png-pipeline skill).
