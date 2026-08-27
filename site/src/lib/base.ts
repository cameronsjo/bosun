// import.meta.env.BASE_URL reflects astro.config.mjs's `base` option exactly
// as configured — for base: '/bosun' that is '/bosun', with NO trailing
// slash. Every page needs a trailing slash to safely concatenate route
// segments (`${base}docs/`), so normalize once here rather than re-deriving
// it (and risking a `/bosundocs/`-shaped bug) in every component.
export const base = import.meta.env.BASE_URL.endsWith('/')
  ? import.meta.env.BASE_URL
  : `${import.meta.env.BASE_URL}/`;
