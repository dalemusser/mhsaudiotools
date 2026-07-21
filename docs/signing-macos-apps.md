For a Wails app distributed outside the Mac App Store, you generally need two things:

1. A Developer ID Application certificate to sign the .app.
2. Apple notarization so Gatekeeper accepts the downloaded app without alarming users.

A plain development certificate is not the right certificate for public distribution.

1. Create the Developer ID Application certificate

Sign in to the Apple Developer portal and go to:

Certificates, Identifiers & Profiles → Certificates → +

Under Software, select:

Developer ID Application

Do not select Mac Development or Apple Distribution for this workflow. A Developer ID Installer certificate is only needed when distributing a signed .pkg installer; it is not needed for an app inside a DMG or ZIP. Apple allows the Account Holder to create up to five Developer ID Application certificates.  

Apple will ask you for a certificate signing request.

Create the certificate signing request

Open Keychain Access on your Mac, then choose:

Keychain Access → Certificate Assistant → Request a Certificate From a Certificate Authority

Enter:

* Your Apple Developer account email
* A common name, such as Intelligence Builders Developer ID
* Leave the CA email blank
* Select Saved to disk

This creates a file ending in:

.certSigningRequest

Upload that file in the Apple Developer portal, download the resulting .cer file, and double-click it to install it in your login keychain. Apple documents the same CSR, download, and Keychain installation process.  

2. Verify that the certificate is installed

Run:

security find-identity -v -p codesigning

You should see something resembling:

Developer ID Application: Intelligence Builders LLC (ABCDE12345)

The value in parentheses is your Apple Developer Team ID.

You can also inspect it in:

Keychain Access → My Certificates

The certificate must have an expandable private key underneath it. If there is no private key, the certificate cannot sign anything. This commonly happens when the CSR was generated on another Mac.

3. Build the Wails app

For Wails 2:

wails build -platform darwin/universal

Or, for Apple Silicon only:

wails build -platform darwin/arm64

The resulting bundle will normally be under:

build/bin/YourApp.app

A universal build is usually best for public distribution because it runs natively on both Intel and Apple Silicon Macs.

4. Configure your bundle identifier

Give the application a reverse-domain bundle ID, for example:

com.intelligencebuilders.myapp

For Wails 2, review the macOS configuration under:

build/darwin/Info.plist

The value should appear as:

<key>CFBundleIdentifier</key>
<string>com.intelligencebuilders.myapp</string>

The bundle identifier should remain stable across releases.

5. Create an entitlements file

For a basic Wails application, start with a file such as:

build/darwin/entitlements.plist

Containing:

<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "https://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
</dict>
</plist>

Many standard Wails apps do not require special entitlements. Add them only when your app actually needs capabilities such as camera, microphone, automation, notifications, or network extensions.

Do not casually add permissive entitlements such as:

com.apple.security.cs.disable-library-validation
com.apple.security.cs.allow-unsigned-executable-memory

They should be used only when required by a component in your application.

6. Sign the Wails application

Set the identity:

SIGNING_IDENTITY="Developer ID Application: Intelligence Builders LLC (ABCDE12345)"
APP="build/bin/YourApp.app"

Then sign:

codesign \
  --force \
  --deep \
  --options runtime \
  --timestamp \
  --entitlements build/darwin/entitlements.plist \
  --sign "$SIGNING_IDENTITY" \
  "$APP"

The important options are:

* --options runtime: enables Apple’s hardened runtime
* --timestamp: obtains a trusted timestamp
* --sign: specifies your Developer ID identity

--deep is convenient for a typical Wails bundle, though for complicated bundles containing frameworks, helpers, plug-ins, or other nested code, the more rigorous approach is to sign every nested executable from the inside out and then sign the outer .app.

Do not alter the application bundle after signing. Apple notes that modifying a bundle after it is signed invalidates its signature and can cause notarization to fail.  

7. Verify the signature locally

Run:

codesign --verify --deep --strict --verbose=2 "$APP"

Inspect the signature:

codesign -dvvv "$APP"

Test how Gatekeeper evaluates it:

spctl --assess --type execute --verbose=4 "$APP"

Before notarization, the signature may be valid while Gatekeeper still does not fully accept the downloaded app. That is expected.

8. Set up notarization credentials

Apple currently uses notarytool; older instructions using altool are obsolete. Apple stopped accepting altool notarization uploads in November 2023.  

You need:

* Your Apple ID email
* Your Team ID
* An app-specific password for your Apple ID

Create an app-specific password in your Apple Account security settings. Then store the credentials in your keychain:

xcrun notarytool store-credentials "notary-profile" \
  --apple-id "your-apple-id@example.com" \
  --team-id "ABCDE12345" \
  --password "xxxx-xxxx-xxxx-xxxx"

This avoids putting the password directly into scripts. Wails 3’s current documentation recommends this same notarytool store-credentials workflow.  

9. Package the signed app for notarization

Apple’s notarization service accepts ZIP, DMG, and PKG submissions, not an unwrapped .app directory.

The simplest option is a ZIP created with ditto:

ditto -c -k --keepParent \
  "build/bin/YourApp.app" \
  "build/bin/YourApp.zip"

Using ditto is preferable to an arbitrary ZIP utility because it preserves macOS bundle metadata correctly.

10. Submit it to Apple

xcrun notarytool submit \
  "build/bin/YourApp.zip" \
  --keychain-profile "notary-profile" \
  --wait

A successful result ends with:

status: Accepted

Notarization is an automated security and code-signing check, not Mac App Store review. Apple scans the Developer ID-signed software and issues a notarization ticket that Gatekeeper can recognize.  

If the status is Invalid, retrieve the diagnostic log:

xcrun notarytool log \
  SUBMISSION-ID-HERE \
  --keychain-profile "notary-profile"

11. Staple the notarization ticket

Staple the ticket to the original application:

xcrun stapler staple "build/bin/YourApp.app"

Validate it:

xcrun stapler validate "build/bin/YourApp.app"

Apple’s notarytool and stapler tools are the supported command-line workflow for uploading and attaching notarization results.  

Because the ZIP you uploaded contains the pre-stapled application, recreate your final ZIP after stapling:

rm "build/bin/YourApp.zip"
ditto -c -k --keepParent \
  "build/bin/YourApp.app" \
  "build/bin/YourApp.zip"

You do not normally need to submit this recreated ZIP again. Its contained app already carries the notarization ticket.

12. Perform the final Gatekeeper test

Run:

spctl --assess \
  --type execute \
  --verbose=4 \
  "build/bin/YourApp.app"

You should get output similar to:

accepted
source=Notarized Developer ID

You can also test the exact downloaded artifact on another Mac. This is valuable because Gatekeeper’s quarantine behavior is generally triggered when a file is downloaded through a browser, email client, or similar application.

Complete basic release script

#!/usr/bin/env bash
set -euo pipefail
APP_NAME="YourApp"
APP_PATH="build/bin/${APP_NAME}.app"
ZIP_PATH="build/bin/${APP_NAME}.zip"
IDENTITY="Developer ID Application: Intelligence Builders LLC (ABCDE12345)"
ENTITLEMENTS="build/darwin/entitlements.plist"
NOTARY_PROFILE="notary-profile"
wails build -platform darwin/universal
codesign \
  --force \
  --deep \
  --options runtime \
  --timestamp \
  --entitlements "$ENTITLEMENTS" \
  --sign "$IDENTITY" \
  "$APP_PATH"
codesign --verify --deep --strict --verbose=2 "$APP_PATH"
rm -f "$ZIP_PATH"
ditto -c -k --keepParent \
  "$APP_PATH" \
  "$ZIP_PATH"
xcrun notarytool submit \
  "$ZIP_PATH" \
  --keychain-profile "$NOTARY_PROFILE" \
  --wait
xcrun stapler staple "$APP_PATH"
xcrun stapler validate "$APP_PATH"
rm -f "$ZIP_PATH"
ditto -c -k --keepParent \
  "$APP_PATH" \
  "$ZIP_PATH"
spctl --assess \
  --type execute \
  --verbose=4 \
  "$APP_PATH"
echo "Signed and notarized artifact: $ZIP_PATH"

The end-to-end flow is therefore:

Developer ID certificate
        ↓
Build the Wails .app
        ↓
Sign with hardened runtime and timestamp
        ↓
Verify the signature
        ↓
Package as ZIP or DMG
        ↓
Submit with notarytool
        ↓
Staple the notarization ticket
        ↓
Create the final downloadable artifact

That is what makes a directly distributed Wails application appear to macOS as an identified, untampered, Apple-notarized application rather than an unknown developer build.