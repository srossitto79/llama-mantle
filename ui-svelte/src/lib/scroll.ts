export function shouldAutoScroll({ scrollTop, scrollHeight, clientHeight }: { scrollTop: number; scrollHeight: number; clientHeight: number }): boolean {
  const deltaFromBottom = scrollHeight - scrollTop - clientHeight;
  return deltaFromBottom <= 24;
}
