import { BrowserWindow } from "electrobun/bun";

const port = process.env.PM_PORT ? parseInt(process.env.PM_PORT, 10) : 8080;

const win = new BrowserWindow({
  title: "Photo Manager",
  url: `http://localhost:${port}`,
  frame: { x: 0, y: 0, width: 1440, height: 900 },
});

win.webview.on("dom-ready", () => {
  console.log(`Photo Manager running at http://localhost:${port}`);
});
