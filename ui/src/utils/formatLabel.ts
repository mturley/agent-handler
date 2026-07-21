const UPPERCASE_WORDS: Record<string, string> = {
  pr: "PR",
  ci: "CI",
  api: "API",
}

export function formatEventType(type: string): string {
  return type
    .replace(/_/g, " ")
    .replace(/\b\w+\b/g, (word) => UPPERCASE_WORDS[word] || word)
}
