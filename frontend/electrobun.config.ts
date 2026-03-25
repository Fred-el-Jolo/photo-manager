export default {
  app: {
    name: "Photo Manager",
    identifier: "dev.photo.manager",
    version: "0.1.0",
  },
  build: {
    bun: {
      entrypoint: "src/bun/index.ts",
    },
    views: {
      "curate-ui": {
        entrypoint: "src/curate-ui/app.ts",
      },
    },
    copy: {
      "src/curate-ui/index.html": "views/curate-ui/index.html",
    },
  },
};
