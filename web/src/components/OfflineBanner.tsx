import React from 'react';

interface OfflineBannerProps {
  isOffline: boolean;
  lastSynced: number | null;
}

function formatLastSynced(ts: number | null): string {
  if (ts === null) return 'never';
  const diff = Date.now() - ts;
  const mins = Math.floor(diff / 60000);
  if (mins < 1) return 'just now';
  if (mins < 60) return `${mins}m ago`;
  const hrs = Math.floor(mins / 60);
  if (hrs < 24) return `${hrs}h ago`;
  return `${Math.floor(hrs / 24)}d ago`;
}

export const OfflineBanner: React.FC<OfflineBannerProps> = ({ isOffline, lastSynced }) => {
  if (!isOffline) return null;

  return (
    <div
      role="status"
      className="w-full bg-amber-100 dark:bg-amber-900/30 border-b border-amber-300 dark:border-amber-700 px-4 py-2 text-sm text-amber-800 dark:text-amber-200 flex items-center gap-2"
    >
      <span className="font-medium">Offline</span>
      <span>— showing cached data</span>
      <span className="ml-auto text-xs opacity-75">
        Last synced: {formatLastSynced(lastSynced)}
      </span>
    </div>
  );
};
