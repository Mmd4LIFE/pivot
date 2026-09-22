// Element matchers: toHaveFocus, toHaveAttribute and the rest. Worth the
// dependency for the failure messages alone -- "expected X to have focus, but
// focus was on Y" is the difference between a fixable test failure and a
// puzzle.
import "@testing-library/jest-dom/vitest";

/*
 * The layout APIs jsdom does not implement.
 *
 * jsdom builds a DOM tree and never lays it out: nothing has a size, nothing
 * has a position, nothing scrolls. Anything that floats above the page has to
 * measure something to decide where to go, so Radix's popper components and
 * cmdk both reach for APIs that simply are not there and throw on the first
 * render.
 *
 * Stubbing them is honest here because of what these tests assert. They check
 * roles, names, states and keyboard behavior — semantics, which jsdom does
 * model — and never appearance. A tooltip that renders in the wrong place is a
 * bug these tests cannot catch and were never going to; a tooltip with no
 * accessible name is one they do.
 *
 * Each stub is the smallest thing that lets the component mount. None of them
 * pretends to measure anything: a fake that returned plausible sizes would let
 * a test assert a position that no browser would produce, which is worse than
 * a test that does not look.
 */

// Radix's popper measures its trigger and content; cmdk measures its list.
class ResizeObserverStub implements ResizeObserver {
  observe(): void {}
  unobserve(): void {}
  disconnect(): void {}
}

globalThis.ResizeObserver ??= ResizeObserverStub;

// Select scrolls the checked item into view when it opens.
Element.prototype.scrollIntoView ??= function scrollIntoView() {};

// Radix Select captures the pointer so a press-and-drag selects an item.
Element.prototype.hasPointerCapture ??= function hasPointerCapture() {
  return false;
};
Element.prototype.setPointerCapture ??= function setPointerCapture() {};
Element.prototype.releasePointerCapture ??= function releasePointerCapture() {};

// The reduced-motion and color-scheme queries the base stylesheet uses.
// Reported as unmatched, which is the conservative answer: the tests then see
// the default theme and the animated variant, not a special case.
window.matchMedia ??= function matchMedia(query: string): MediaQueryList {
  return {
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  };
};
