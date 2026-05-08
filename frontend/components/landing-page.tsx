"use client";

import type { CodeTab } from "@/lib/code";
import type { GitHubRepoData } from "@/lib/github";
import { DeferredSection } from "./landing/ui";
import { ScrollProgress, Navigation } from "./landing/nav";
import { Hero } from "./landing/hero";
import { IncidentBanner, SocialProof } from "./landing/social-proof";
import { ProblemSection } from "./landing/problem";
import { MetricsSection } from "./landing/metrics";
import { HowItWorks } from "./landing/how-it-works";
import { CodeIntegration } from "./landing/code-integration";
import { RiskEngineSection } from "./landing/risk-engine";
import { AgentSection } from "./landing/agent";
import { BentoSection } from "./landing/bento";
import { IntegrationsSection } from "./landing/integrations";
import { PricingSection } from "./landing/pricing";
import { TestimonialsSection } from "./landing/testimonials";
import { OpenSourceSection } from "./landing/open-source";
import { CTASection } from "./landing/cta";
import { Footer } from "./landing/footer";

type LandingPageProps = {
  codeTabs: CodeTab[];
  github: GitHubRepoData;
};

export function LandingPage({ codeTabs, github }: LandingPageProps) {
  return (
    <main className="min-h-screen overflow-hidden bg-bg-primary text-text-primary">
      <ScrollProgress />
      <Navigation github={github} />
      <Hero />
      <IncidentBanner />
      <SocialProof />
      <ProblemSection />
      <DeferredSection minHeight={280}><MetricsSection /></DeferredSection>
      <DeferredSection minHeight={620}><HowItWorks /></DeferredSection>
      <DeferredSection minHeight={680}><CodeIntegration codeTabs={codeTabs} /></DeferredSection>
      <DeferredSection minHeight={520}><RiskEngineSection /></DeferredSection>
      <DeferredSection minHeight={560}><AgentSection /></DeferredSection>
      <DeferredSection minHeight={720}><BentoSection /></DeferredSection>
      <DeferredSection minHeight={820}><IntegrationsSection /></DeferredSection>
      <DeferredSection minHeight={720}><PricingSection /></DeferredSection>
      <DeferredSection minHeight={520}><TestimonialsSection /></DeferredSection>
      <DeferredSection minHeight={500}><OpenSourceSection github={github} /></DeferredSection>
      <DeferredSection minHeight={600}><CTASection /></DeferredSection>
      <DeferredSection minHeight={380}><Footer /></DeferredSection>
    </main>
  );
}
