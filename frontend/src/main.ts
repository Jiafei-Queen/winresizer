import { mount } from "svelte";
import App from "./App.svelte";
import Overlay from "./Overlay.svelte";

// The overlay picker reuses the same bundle via hash routing: the backend
// opens a transparent window at "/#overlay" while the console lives at "/".
const target = document.getElementById("app")!;
if (window.location.hash === "#overlay") {
  // Overlay-only page class: scopes Overlay.svelte's html/body rules to
  // this page so they never leak into the console (see its <style>).
  document.documentElement.classList.add("overlay-page");
  mount(Overlay, { target });
} else {
  document.documentElement.classList.add("console-page");
  mount(App, { target });
}
