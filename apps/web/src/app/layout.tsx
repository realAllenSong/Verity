import type { Metadata } from "next";
import { AppTheme } from "@/components/app-theme";
import { GeistMono } from "geist/font/mono";
import { GeistSans } from "geist/font/sans";
import "@radix-ui/themes/styles.css";
import "./globals.css";
import "./workbench.css";

export const metadata: Metadata = {
  title: "Verity | White-box data preparation",
  description: "Inspect, clean, review, and publish model-ready datasets with complete decision lineage.",
};

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <html
      lang="en"
      className={`${GeistSans.variable} ${GeistMono.variable}`}
      suppressHydrationWarning
    >
      <body>
        <AppTheme>
          {children}
        </AppTheme>
      </body>
    </html>
  );
}
