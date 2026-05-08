"use client";

import {
  siClaude, siCursor, siDocker, siGithub, siLangchain,
  siLinear, siReplit, siSupabase, siVercel, siWindsurf
} from "simple-icons";

const OPENAI_PATH =
  "M22.2819 9.8211a5.9847 5.9847 0 0 0-.5157-4.9108 6.0462 6.0462 0 0 0-6.5098-2.9A6.0651 6.0651 0 0 0 4.9807 4.1818a5.9847 5.9847 0 0 0-3.9977 2.9 6.0462 6.0462 0 0 0 .7427 7.0966 5.98 5.98 0 0 0 .511 4.9107 6.051 6.051 0 0 0 6.5146 2.9001A5.9847 5.9847 0 0 0 13.2599 24a6.0557 6.0557 0 0 0 5.7718-4.2058 5.9894 5.9894 0 0 0 3.9977-2.9001 6.0557 6.0557 0 0 0-.7475-7.0729zm-9.022 12.6081a4.4755 4.4755 0 0 1-2.8764-1.0408l.1419-.0804 4.7783-2.7582a.7948.7948 0 0 0 .3927-.6813v-6.7369l2.02 1.1686a.071.071 0 0 1 .038.052v5.5826a4.504 4.504 0 0 1-4.4945 4.4944zm-9.6607-4.1254a4.4708 4.4708 0 0 1-.5346-3.0137l.142.0852 4.783 2.7582a.7712.7712 0 0 0 .7806 0l5.8428-3.3685v2.3324a.0804.0804 0 0 1-.0332.0615L9.74 19.9502a4.4992 4.4992 0 0 1-6.1408-1.6464zM2.3408 7.8956a4.485 4.485 0 0 1 2.3655-1.9728V11.6a.7664.7664 0 0 0 .3879.6765l5.8144 3.3543-2.0201 1.1685a.0757.0757 0 0 1-.071 0l-4.8303-2.7865A4.504 4.504 0 0 1 2.3408 7.872zm16.5963 3.8558L13.1038 8.364 15.1192 7.2a.0757.0757 0 0 1 .071 0l4.8303 2.7913a4.4944 4.4944 0 0 1-.6765 8.1042v-5.6772a.79.79 0 0 0-.407-.667zm2.0107-3.0231l-.142-.0852-4.7735-2.7818a.7759.7759 0 0 0-.7854 0L9.409 9.2297V6.8974a.0662.0662 0 0 1 .0284-.0615l4.8303-2.7866a4.4992 4.4992 0 0 1 6.6802 4.66zM8.3065 12.863l-2.02-1.1638a.0804.0804 0 0 1-.038-.0567V6.0742a4.4992 4.4992 0 0 1 7.3757-3.4537l-.142.0805L8.704 5.459a.7948.7948 0 0 0-.3927.6813zm1.0976-2.3654l2.602-1.4998 2.6069 1.4998v2.9994l-2.5974 1.4997-2.6067-1.4997Z";

const ANTIGRAVITY_PATH =
  "M12.006 3.02q1.736-.02 2.682 1.303q.945 1.323 1.648 2.985q.612 1.46 1.212 3.288q.6 1.829 1.256 3.683q.65 1.779 1.387 3.342q.736 1.564 1.703 2.643q.123.148.118.323t-.13.284q-.142.129-.31.135t-.31-.117q-1.745-1.627-2.933-3.434q-1.189-1.807-2.296-3.02q-.85-.95-1.823-1.51q-.972-.56-2.204-.54q-1.233-.02-2.205.54t-1.822 1.51q-1.108 1.213-2.296 3.02T2.75 20.889q-.14.123-.31.117t-.311-.135q-.123-.11-.129-.284t.117-.323q.967-1.06 1.704-2.637t1.387-3.352q.655-1.854 1.255-3.683t1.212-3.288q.704-1.662 1.649-2.985t2.682-1.304";

const brandLogos = [
  { path: siClaude.path,    hex: siClaude.hex,    name: "Claude"       },
  { path: OPENAI_PATH,      hex: "10A37F",         name: "Codex"        },
  { path: ANTIGRAVITY_PATH, hex: "5F89C0",         name: "AntiGravity"  },
  { path: siCursor.path,    hex: siCursor.hex,    name: "Cursor"       },
  { path: siWindsurf.path,  hex: siWindsurf.hex,  name: "Windsurf"     },
  { path: siLangchain.path, hex: siLangchain.hex, name: "LangChain"    },
  { path: siGithub.path,    hex: siGithub.hex,    name: "GitHub"       },
  { path: siVercel.path,    hex: siVercel.hex,    name: "Vercel"       },
  { path: siReplit.path,    hex: siReplit.hex,    name: "Replit"       },
  { path: siSupabase.path,  hex: siSupabase.hex,  name: "Supabase"     },
  { path: siDocker.path,    hex: siDocker.hex,    name: "Docker"       },
  { path: siLinear.path,    hex: siLinear.hex,    name: "Linear"       },
];

export function IncidentBanner() {
  const text = "A coding-session AI agent deleted a production database. Backstop is built to reduce this class of failure with interception, approval, and recovery readiness.";
  return (
    <section id="incident" className="marquee-wrap overflow-hidden border-y border-[rgba(255,68,68,0.3)] bg-[#0a0404] py-3">
      <div style={{ perspective: "320px", perspectiveOrigin: "50% 50%" }}>
        <div style={{ transform: "rotateX(12deg)", transformOrigin: "50% 50%", backfaceVisibility: "hidden" }}>
          <div className="marquee-track gap-8 whitespace-nowrap font-mono text-sm text-text-secondary">
            {[0, 1, 2, 3].map((item) => (
              <div key={item} className="flex items-center gap-8">
                <span>{text}</span>
                <span className="h-1.5 w-1.5 rounded-full bg-danger" />
                <span className="text-text-primary">Read the incident, then design for guardrails.</span>
                <span className="h-1.5 w-1.5 rounded-full bg-danger" />
                <a href="https://www.theregister.com/2026/04/27/cursoropus_agent_snuffs_out_pocketos/" target="_blank" rel="noopener noreferrer" className="text-danger">Read the full post-mortem →</a>
              </div>
            ))}
          </div>
        </div>
      </div>
    </section>
  );
}

export function SocialProof() {
  const repeated = [...brandLogos, ...brandLogos, ...brandLogos];
  return (
    <section className="border-b border-border py-10">
      <p className="mb-7 text-center text-[11px] uppercase tracking-[0.22em] text-text-tertiary">
        Built for teams shipping with AI agents
      </p>
      <div className="mx-auto max-w-7xl px-4 sm:px-6 lg:px-8">
        <div className="fade-mask overflow-hidden">
          <div className="marquee-track [animation-duration:56s]">
            {repeated.map((brand, index) => (
              <div
                key={`${brand.name}-${index}`}
                className="group flex min-w-fit items-center gap-2 px-7 py-3 opacity-40 grayscale transition duration-300 hover:opacity-90 hover:grayscale-0"
                aria-label={brand.name}
              >
                <svg
                  role="img"
                  viewBox="0 0 24 24"
                  className="h-[18px] w-[18px] shrink-0 transition-colors duration-300"
                  style={{ fill: `#${brand.hex}` }}
                  aria-hidden="true"
                >
                  <path d={brand.path} />
                </svg>
                <span className="whitespace-nowrap font-mono text-sm text-text-secondary transition-colors duration-300 group-hover:text-text-primary">
                  {brand.name}
                </span>
              </div>
            ))}
          </div>
        </div>
      </div>
    </section>
  );
}
