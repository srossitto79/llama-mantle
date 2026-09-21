import { describe, expect, it } from 'vitest';
import { shouldAutoScroll } from './scroll';

describe('shouldAutoScroll', () => {
  it('keeps the tail visible when the user is already at the bottom', () => {
    expect(shouldAutoScroll({ scrollTop: 320, scrollHeight: 500, clientHeight: 180 })).toBe(true);
  });

  it('stops auto-scrolling once the user scrolled away from the bottom', () => {
    expect(shouldAutoScroll({ scrollTop: 120, scrollHeight: 500, clientHeight: 180 })).toBe(false);
  });
});
