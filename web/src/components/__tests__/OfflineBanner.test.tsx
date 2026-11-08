import "fake-indexeddb/auto";
import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { OfflineBanner } from '../OfflineBanner';

describe('OfflineBanner', () => {
  it('renders nothing when online', () => {
    const { container } = render(<OfflineBanner isOffline={false} lastSynced={null} />);
    expect(container).toBeEmptyDOMElement();
  });

  it('shows offline message when offline', () => {
    render(<OfflineBanner isOffline={true} lastSynced={Date.now() - 60000} />);
    expect(screen.getByText(/offline/i)).toBeInTheDocument();
    expect(screen.getByText(/last synced/i)).toBeInTheDocument();
  });
});
