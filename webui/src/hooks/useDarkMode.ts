import { useState, useEffect } from 'react';

type Theme = 'light' | 'dark' | 'system';

export function useDarkMode(): {
  theme: Theme;
  isDark: boolean;
  setTheme: (theme: Theme) => void;
  toggleDarkMode: () => void;
} {
  const [theme, setThemeState] = useState<Theme>(() => {
    const stored = localStorage.getItem('theme') as Theme | null;
    return stored || 'system';
  });

  const [isDark, setIsDark] = useState<boolean>(() => {
    if (theme === 'system') {
      return window.matchMedia('(prefers-color-scheme: dark)').matches;
    }
    return theme === 'dark';
  });

  useEffect(() => {
    const updateDarkMode = (): void => {
      let shouldBeDark: boolean;
      if (theme === 'system') {
        shouldBeDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
      } else {
        shouldBeDark = theme === 'dark';
      }

      setIsDark(shouldBeDark);

      if (shouldBeDark) {
        document.documentElement.classList.add('dark');
      } else {
        document.documentElement.classList.remove('dark');
      }
    };

    updateDarkMode();

    // Listen for system preference changes
    const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)');
    const handler = (): void => {
      if (theme === 'system') {
        updateDarkMode();
      }
    };

    mediaQuery.addEventListener('change', handler);
    return () => mediaQuery.removeEventListener('change', handler);
  }, [theme]);

  const setTheme = (newTheme: Theme): void => {
    localStorage.setItem('theme', newTheme);
    setThemeState(newTheme);
  };

  const toggleDarkMode = (): void => {
    setTheme(isDark ? 'light' : 'dark');
  };

  return { theme, isDark, setTheme, toggleDarkMode };
}
