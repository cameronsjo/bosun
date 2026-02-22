# Bosun Transparent

Stylized chibi nautical officer optimized for transparent background compositing over dark UIs and terminal aesthetics.

## Palette

- **Background**: Plain solid black (#000000) -- removed in post-processing
- **Peacoat**: Deep navy blue (#1B2A4A) with subtle fabric texture
- **Brass**: Warm metallic gold (#C5943A) for buttons, buckles, whistle accents
- **Whistle**: Polished silver (#B8C4D0) with amber glow (#F5A623)
- **Skin**: Warm tan (#D4A574) with soft subsurface scattering
- **Rope**: Natural hemp tan (#C4A66A)
- **Rim lighting**: Dual-color -- cool cyan (#4FC3F7) from the left, warm amber (#FFB74D) from the right

## Medium

Clean 3D render with stylized proportions. Smooth surfaces with material differentiation: matte woven texture on the peacoat, metallic sheen on brass buttons and whistle, soft translucency on skin, rough fiber texture on rope. Pixar-adjacent quality -- not photorealistic, not flat illustration. Subtle ambient occlusion in fabric folds and under the collar.

## Mood

Calm competence. Steady. The person who keeps things running.

## Imperfections

Minimal -- clean edges required for background removal. Soft bloom on the whistle's amber glow only. No atmospheric haze, no particles near silhouette edges. Fabric wrinkles are stylized, not noisy.

## Text Treatment

NO text in generated images. All text handled by the compositing target.

## Composition

- **Subject-focused**: Bosun character with minimal props, no environment
- Square aspect ratio (1:1)
- Subject fills ~60-70% of the frame vertically
- Plain solid black background with NO gradients, NO environment, NO floor reflections
- Props/effects kept close to the subject (rope over shoulder, whistle at chest)
- Clean silhouette edges -- dual rim lighting (cyan left, amber right) provides strong separation from black background
- No ground plane, no shadow on the ground, no floor

## Post-Processing

Images generated with solid black backgrounds, then processed to transparent PNG using rembg (see transparent-png-pipeline skill).
