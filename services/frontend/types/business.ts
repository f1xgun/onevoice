export interface ScheduleDay {
  day: 'mon' | 'tue' | 'wed' | 'thu' | 'fri' | 'sat' | 'sun';
  open: string; // "09:00"
  close: string; // "21:00"
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
  schedule?: ScheduleDay[];
}
