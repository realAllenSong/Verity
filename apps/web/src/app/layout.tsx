import type { Metadata } from "next";
import { Theme } from "@radix-ui/themes";
import { GeistMono } from "geist/font/mono";
import { GeistSans } from "geist/font/sans";
import "@radix-ui/themes/styles.css";
import "./globals.css";

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
        <Theme accentColor="blue" grayColor="slate" radius="medium" scaling="100%">
          {children}
        </Theme>
      </body>
    </html>
  );
}
