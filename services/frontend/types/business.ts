export interface ScheduleDay {
  day: 'mon' | 'tue' | 'wed' | 'thu' | 'fri' | 'sat' | 'sun';
  open: string; // "09:00"
  close: string; // "21:00"
  closed: boolean;
}

export interface SpecialDate {
  date: string; // "2026-03-08" ISO format
  open?: string; // "10:00" — if absent, means closed
  close?: string; // "15:00"
  closed: boolean;
}

export interface Business {
  id: string;
  name: string;
  category: string;
  phone?: string;
  // The API serialises `website` as `null` when unset (domain.Business.Website
  // is a *string), unlike the other optional string fields which come back "".
  website?: string | null;
  description?: string;
  logoUrl?: string;
  address?: string;
  settings?: Record<string, unknown>;
  schedule?: ScheduleDay[];
}
