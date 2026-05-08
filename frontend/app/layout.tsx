import type { Metadata } from "next";
import { GeistMono, GeistSans } from "geist/font";
import { Instrument_Serif, Space_Grotesk } from "next/font/google";
import "./globals.css";

const instrumentSerif = Instrument_Serif({
  subsets: ["latin"],
  weight: "400",
  style: ["normal", "italic"],
  variable: "--font-serif",
  display: "swap"
});

const spaceGrotesk = Space_Grotesk({
  subsets: ["latin"],
  weight: ["600", "700"],
  variable: "--font-brand",
  display: "swap"
});

export const metadata: Metadata = {
  title: "Backstop | Database safety for developers and AI agents",
  description:
    "Classify risky PostgreSQL queries, require approval when needed, verify recovery readiness, and restore supported table snapshots from your own storage."
};

export default function RootLayout({
  children
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en" className={`${GeistSans.variable} ${GeistMono.variable} ${instrumentSerif.variable} ${spaceGrotesk.variable}`} suppressHydrationWarning>
      <body suppressHydrationWarning>{children}</body>
    </html>
  );
}
