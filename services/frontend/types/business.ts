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
  website?: string;
  description?: string;
  logo_url?: string;
  address?: string;
  settings?: Record<string, unknown>;
  schedule?: ScheduleDay[];
}
