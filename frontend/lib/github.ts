import { formatDistanceToNowStrict } from "date-fns";

export type GitHubRepoData = {
  name: string;
  url: string;
  stars: number | null;
  forks: number | null;
  license: string | null;
  lastCommitMessage: string | null;
  lastCommitRelative: string | null;
  contributors: { login: string; avatarUrl: string; url: string }[];
};

const fallback: GitHubRepoData = {
  name: "github.com/pratyush2514/Backstop",
  url: "https://github.com/pratyush2514/Backstop",
  stars: null,
  forks: null,
  license: "Apache-2.0",
  lastCommitMessage: "Live GitHub data unavailable",
  lastCommitRelative: null,
  contributors: []
};

export async function getGitHubRepoData(): Promise<GitHubRepoData> {
  const repo = process.env.BACKSTOP_GITHUB_REPO ?? "pratyush2514/Backstop";
  const headers: HeadersInit = {
    Accept: "application/vnd.github+json",
    "X-GitHub-Api-Version": "2022-11-28"
  };

  if (process.env.GITHUB_TOKEN) {
    headers.Authorization = `Bearer ${process.env.GITHUB_TOKEN}`;
  }

  try {
    const [repoResponse, commitResponse, contributorResponse] = await Promise.all([
      fetch(`https://api.github.com/repos/${repo}`, { headers, next: { revalidate: 300 } }),
      fetch(`https://api.github.com/repos/${repo}/commits?per_page=1`, { headers, next: { revalidate: 300 } }),
      fetch(`https://api.github.com/repos/${repo}/contributors?per_page=5`, {
        headers,
        next: { revalidate: 300 }
      })
    ]);

    if (!repoResponse.ok) {
      return { ...fallback, name: `github.com/${repo}`, url: `https://github.com/${repo}` };
    }

    const repoJson = await repoResponse.json();
    const commitJson = commitResponse.ok ? await commitResponse.json() : [];
    const contributorJson = contributorResponse.ok ? await contributorResponse.json() : [];
    const commit = Array.isArray(commitJson) ? commitJson[0] : null;
    const commitDate = commit?.commit?.committer?.date ? new Date(commit.commit.committer.date) : null;

    return {
      name: `github.com/${repo}`,
      url: repoJson.html_url ?? `https://github.com/${repo}`,
      stars: typeof repoJson.stargazers_count === "number" ? repoJson.stargazers_count : null,
      forks: typeof repoJson.forks_count === "number" ? repoJson.forks_count : null,
      license: repoJson.license?.spdx_id ?? "Apache-2.0",
      lastCommitMessage: commit?.commit?.message?.split("\n")[0] ?? "Live commit data unavailable",
      lastCommitRelative: commitDate ? `${formatDistanceToNowStrict(commitDate)} ago` : null,
      contributors: Array.isArray(contributorJson)
        ? contributorJson.slice(0, 5).map((person: any) => ({
            login: person.login,
            avatarUrl: person.avatar_url,
            url: person.html_url
          }))
        : []
    };
  } catch {
    return { ...fallback, name: `github.com/${repo}`, url: `https://github.com/${repo}` };
  }
}

