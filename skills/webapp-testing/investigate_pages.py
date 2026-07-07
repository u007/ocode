#!/usr/bin/env python3
"""
Investigate front3 pages to understand the actual DOM structure and selectors
"""
from playwright.sync_api import sync_playwright
import json

def investigate_page(page, url, name):
    """Visit a page and extract useful information"""
    print(f"\n{'='*60}")
    print(f"Investigating: {name}")
    print(f"URL: {url}")
    print('='*60)

    try:
        page.goto(url, wait_until='networkidle', timeout=10000)
        page.wait_for_timeout(2000)  # Extra wait for dynamic content

        # Take screenshot
        screenshot_path = f'/tmp/front3_{name}.png'
        page.screenshot(path=screenshot_path, full_page=True)
        print(f"Screenshot saved: {screenshot_path}")

        # Get page title
        title = page.title()
        print(f"Page title: {title}")

        # Check for common form elements
        print("\n--- Form Elements ---")
        inputs = page.locator('input').all()
        print(f"Total inputs: {len(inputs)}")
        for i, inp in enumerate(inputs[:10]):  # Limit to first 10
            inp_type = inp.get_attribute('type') or 'text'
            inp_id = inp.get_attribute('id') or ''
            inp_name = inp.get_attribute('name') or ''
            inp_placeholder = inp.get_attribute('placeholder') or ''
            print(f"  Input {i+1}: type={inp_type}, id={inp_id}, name={inp_name}, placeholder={inp_placeholder}")

        # Check for buttons
        buttons = page.locator('button').all()
        print(f"\nTotal buttons: {len(buttons)}")
        for i, btn in enumerate(buttons[:10]):
            btn_text = btn.text_content().strip()
            btn_type = btn.get_attribute('type') or ''
            print(f"  Button {i+1}: text='{btn_text}', type={btn_type}")

        # Check for links
        links = page.locator('a[href]').all()
        print(f"\nTotal links: {len(links[:20])}")
        for i, link in enumerate(links[:10]):
            href = link.get_attribute('href')
            text = link.text_content().strip()
            print(f"  Link {i+1}: href={href}, text='{text}'")

        # Check for specific data-testid attributes
        testids = page.locator('[data-testid]').all()
        print(f"\nTotal elements with data-testid: {len(testids)}")
        for i, elem in enumerate(testids[:10]):
            testid = elem.get_attribute('data-testid')
            tag = elem.evaluate('el => el.tagName').lower()
            print(f"  {i+1}: <{tag} data-testid='{testid}'>")

        # Get current URL (after any redirects)
        current_url = page.url
        if current_url != url:
            print(f"\nRedirected to: {current_url}")

        return True

    except Exception as e:
        print(f"ERROR: {e}")
        return False

def main():
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        page = browser.new_page()

        # Pages to investigate
        pages = [
            ('http://localhost:3001/signup', 'signup'),
            ('http://localhost:3001/login', 'login'),
            ('http://localhost:3001/products', 'products'),
            ('http://localhost:3001/cart', 'cart'),
            ('http://localhost:3001/checkout', 'checkout'),
        ]

        for url, name in pages:
            investigate_page(page, url, name)

        # Try authenticated pages (login first)
        print("\n" + "="*60)
        print("Logging in to access authenticated pages...")
        print("="*60)

        try:
            page.goto('http://localhost:3001/login', wait_until='networkidle')
            page.wait_for_timeout(1000)

            # Fill login form
            email_input = page.locator('#email')
            if email_input.count() > 0:
                email_input.fill('test@example.com')
                page.locator('#password').fill('TestPassword123!')
                page.locator('button[type="submit"]').click()
                page.wait_for_timeout(3000)

                print("Logged in successfully!")

                # Investigate authenticated pages
                auth_pages = [
                    ('http://localhost:3001/account/profile', 'account_profile'),
                    ('http://localhost:3001/account/orders', 'account_orders'),
                    ('http://localhost:3001/account/addresses', 'account_addresses'),
                ]

                for url, name in auth_pages:
                    investigate_page(page, url, name)
            else:
                print("Could not find login form")

        except Exception as e:
            print(f"Login failed: {e}")

        browser.close()
        print("\n" + "="*60)
        print("Investigation complete!")
        print("="*60)

if __name__ == '__main__':
    main()
