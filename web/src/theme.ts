// The page follows the operating system's light/dark setting.
export type Theme = "light" | "dark" | "system";

export function useTheme(): { theme: Theme } {
  return { theme: "system" };
}

/** Puts the `dark` class on <html> to match the system, and keeps it in step. */
export function applySystemTheme(): void {
  const q = window.matchMedia("(prefers-color-scheme: dark)");
  const apply = () => document.documentElement.classList.toggle("dark", q.matches);
  apply();
  q.addEventListener("change", apply);
}
