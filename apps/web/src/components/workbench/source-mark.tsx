import {
  CalendarBlankIcon,
  GithubLogoIcon,
  MicrosoftOutlookLogoIcon,
  MicrosoftPowerpointLogoIcon,
  OpenAiLogoIcon,
  PlugsConnectedIcon,
  SlackLogoIcon,
} from "@phosphor-icons/react";
import { siAnthropic, siConfluence, siJira } from "simple-icons";
import type { SourceName } from "@/lib/contracts";

const phosphorMarks = {
  codex: OpenAiLogoIcon,
  github: GithubLogoIcon,
  slack: SlackLogoIcon,
  outlook: MicrosoftOutlookLogoIcon,
  calendar: CalendarBlankIcon,
  powerpoint: MicrosoftPowerpointLogoIcon,
} as const;

const simpleMarks = {
  claude_code: siAnthropic,
  jira: siJira,
  confluence: siConfluence,
} as const;

const sourceColors: Record<string, { foreground: string; background: string }> = {
  codex: { foreground: "#111827", background: "#edf0f4" },
  claude_code: { foreground: "#c75f3a", background: "#fff0e9" },
  github: { foreground: "#24292f", background: "#edf0f4" },
  jira: { foreground: "#1868db", background: "#eaf2ff" },
  slack: { foreground: "#611f69", background: "#f7ecf8" },
  outlook: { foreground: "#0a64ad", background: "#e9f4ff" },
  calendar: { foreground: "#3056d3", background: "#edf2ff" },
  confluence: { foreground: "#1868db", background: "#eaf2ff" },
  powerpoint: { foreground: "#c43e1c", background: "#fff0eb" },
};

export function SourceMark({ source }: { source: SourceName }) {
  const colors = sourceColors[source] ?? { foreground: "#34415a", background: "#eef2f6" };
  const simple = simpleMarks[source as keyof typeof simpleMarks];
  const Icon = phosphorMarks[source as keyof typeof phosphorMarks] ?? PlugsConnectedIcon;

  return (
    <span
      className="source-mark"
      style={{ "--source-foreground": colors.foreground, "--source-background": colors.background } as React.CSSProperties}
    >
      {simple ? (
        <svg aria-hidden="true" viewBox="0 0 24 24">
          <path d={simple.path} />
        </svg>
      ) : (
        <Icon aria-hidden="true" size={17} weight="fill" />
      )}
    </span>
  );
}
