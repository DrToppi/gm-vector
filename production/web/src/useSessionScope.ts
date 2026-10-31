import { useState } from "react";
import { sessionScope } from "./api";
// Bind callbacks to the session in which this component was mounted. Async file
// reads or chained operations must not silently adopt a later user's cookie.
export function useSessionScope() {
  return useState(sessionScope)[0];
}
