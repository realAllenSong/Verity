"use client";

import { useSyncExternalStore, type ReactNode } from "react";
import { Theme } from "@radix-ui/themes";

const query = "(prefers-color-scheme: dark)";
function subscribe(onChange: () => void) {
  const media = window.matchMedia(query);
  media.addEventListener("change", onChange);
  return () => media.removeEventListener("change", onChange);
}

// The server and initial hydration agree; system preference updates after hydration.
export function AppTheme({ children }: { children: ReactNode }) {
  const dark = useSyncExternalStore(subscribe, () => window.matchMedia(query).matches, () => false);
  return <Theme appearance={dark ? "dark" : "light"} accentColor="blue" grayColor="slate" radius="medium" scaling="100%">{children}</Theme>;
}
