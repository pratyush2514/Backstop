import { LandingPage } from "@/components/landing-page";
import { getCodeTabs } from "@/lib/code";
import { getGitHubRepoData } from "@/lib/github";

export const revalidate = 300;

export default async function Page() {
  const [codeTabs, github] = await Promise.all([getCodeTabs(), getGitHubRepoData()]);

  return <LandingPage codeTabs={codeTabs} github={github} />;
}
