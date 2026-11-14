import { cleanup } from "@testing-library/react";
import { afterEach } from "vite-plus/test";

// jsdom does not implement scrolling; the router restores the scroll position on every navigation.
window.scrollTo = () => {};

afterEach(() => {
  cleanup();
});
