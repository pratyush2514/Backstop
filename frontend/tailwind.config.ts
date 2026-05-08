import type { Config } from "tailwindcss";

const config: Config = {
  content: [
    "./app/**/*.{ts,tsx}",
    "./components/**/*.{ts,tsx}",
    "./lib/**/*.{ts,tsx}",
    "./mdx-components.tsx",
  ],
  theme: {
    extend: {
      fontFamily: {
        sans: ["var(--font-geist-sans)", "Inter", "system-ui", "sans-serif"],
        mono: ["var(--font-geist-mono)", "JetBrains Mono", "ui-monospace", "SFMono-Regular", "monospace"],
        serif: ["var(--font-serif)", "Georgia", "serif"],
      },
      colors: {
        bg: {
          primary: "#09090b", // Deep immersive black
          secondary: "#121214", // Subtle elevation
          elevated: "#18181b", // Cards
        },
        text: {
          primary: "#fafafa", // Off-white, high contrast
          secondary: "#a1a1aa", // Muted for long reading
          tertiary: "#71717a", // Borders/lines
        },
        danger: "#ff4444",
        warning: "#eab308",
        success: "#22c55e",
        info: "#3b82f6",
        border: "rgba(255, 255, 255, 0.08)",
      },
      animation: {
        "fade-in": "fadeIn 0.4s ease-out forwards",
        "slide-up": "slideUp 0.4s cubic-bezier(0.16, 1, 0.3, 1) forwards",
        "pulse-slow": "pulse 3s cubic-bezier(0.4, 0, 0.6, 1) infinite",
        "border-beam": "beam-spin 3s linear infinite",
        "aurora-breathe": "aurora-breathe 15s ease-in-out infinite alternate",
      },
      keyframes: {
        fadeIn: {
          "0%": { opacity: "0" },
          "100%": { opacity: "1" },
        },
        slideUp: {
          "0%": { opacity: "0", transform: "translateY(12px)" },
          "100%": { opacity: "1", transform: "translateY(0)" },
        },
        "beam-spin": {
          "100%": { "--beam-angle": "360deg" },
        },
      },
      boxShadow: {
        danger: "0 0 20px rgba(255,68,68,0.18)",
        dangerSoft: "0 0 60px rgba(255,68,68,0.08)",
      },
      typography: {
        DEFAULT: {
          css: {
            color: '#a1a1aa',
            a: { color: '#ff4444', '&:hover': { color: '#ff5f56' } },
            h1: { color: '#fafafa' },
            h2: { color: '#fafafa' },
            h3: { color: '#fafafa' },
            h4: { color: '#fafafa' },
            strong: { color: '#fafafa' },
            code: { color: '#ff4444' },
          },
        },
      },
    },
  },
  plugins: [],
};

export default config;
