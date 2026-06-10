/// <reference types="vite/client" />

declare module "*.css" {
  const content: string;
  export default content;
}

declare module "@picocss/pico" {
  const content: string;
  export default content;
}