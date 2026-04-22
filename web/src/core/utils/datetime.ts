import { formatDistanceToNow, isValid, parseISO } from "date-fns";
import { enUS as dateFnsEnUS, zhCN as dateFnsZhCN } from "date-fns/locale";

import { detectLocale, type Locale } from "@/core/i18n";
import { getLocaleFromCookie } from "@/core/i18n/cookies";

function getDateFnsLocale(locale: Locale) {
  switch (locale) {
    case "zh-CN":
      return dateFnsZhCN;
    case "en-US":
    default:
      return dateFnsEnUS;
  }
}

export function formatTimeAgo(date: Date | string | number, locale?: Locale) {
  // Handle empty/invalid dates gracefully
  if (!date) {
    return "-";
  }

  let parsedDate: Date;
  if (typeof date === "string") {
    parsedDate = parseISO(date);
  } else if (typeof date === "number") {
    parsedDate = new Date(date);
  } else {
    parsedDate = date;
  }

  if (!isValid(parsedDate)) {
    return "-";
  }

  const effectiveLocale =
    locale ??
    (getLocaleFromCookie() as Locale | null) ??
    // Fallback when cookie is missing (or on first render)
    detectLocale();
  return formatDistanceToNow(parsedDate, {
    addSuffix: true,
    locale: getDateFnsLocale(effectiveLocale),
  });
}
