/// <reference types="bun-types" />

declare module '*.sql' {
  const sql: string
  export default sql
}
