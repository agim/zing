# Brief: zing

What this app is for, who owns its data, how it looks, what it uses,
where it runs and how agents work on it, in the words of the people who
own it. Agents read it before building anything. The kickoff interview
fills it: `lidza brief` in a terminal, or the recipe "Start with the
brief" in an agent session (MCP `lidza_brief`, `lidza_brief_answer`).
Change an answer the same way, or edit it here.

## Product

### What does the app do, in one sentence? <!-- brief:purpose -->

Self-hosted Zing social platform that hosts its own users and social content, with server-side interoperability with Mastodon/ActivityPub and Bluesky/AT Protocol. Connecting existing external accounts is an additional feature, not the core product. The choice between established server backends and a native Zing federation implementation is pending the owner’s answer.

### Who uses it? <!-- brief:users -->

Public open-source repository; each installation chooses public/private access. The owner instance is private at zing.albaspot.com via lidza-deploy.albaspot.com.

### What are the three to five things people come to do? <!-- brief:journeys -->

Register and verify email; create a hosted social identity; publish and interact locally without an external social account; discover and interact with remote users through supported protocols; compose with images, drafts and schedules; manage privacy and moderation. Existing network connections remain supplementary.

### What is out of scope for now? <!-- brief:out_of_scope -->

_Open._

## Data

### Who owns the data, and who may see it? <!-- brief:ownership -->

Each user exclusively owns their connections, credentials, drafts, media, schedules and deliveries. Realtime topics require the same owner.

### Which roles are there? <!-- brief:roles -->

_Open._

### Which personal data does it keep? <!-- brief:personal_data -->

_Open._

### What happens to an account's data when it is deleted? <!-- brief:retention -->

_Open._

## Accounts

### How do people sign in? <!-- brief:signin -->

Independent email/password through Lidza auth.Mount, email verification, reset, sessions and account deletion; social connection is separate.

### Who may create an account? <!-- brief:registration -->

_Open._

## Design

### Which palette? <!-- brief:palette -->

Use supplied Zing SVGs and electric blue #0085F3, black and white; follow device theme with manual override.

### Which typography? <!-- brief:typography -->

_Open._

### What should it feel like? <!-- brief:mood -->

_Open._

### Light and dark? <!-- brief:themes -->

_Open._

### Which languages? <!-- brief:languages -->

_Open._

### Which currency and time zone? <!-- brief:locale -->

_Open._

## Content

### Where does the content come from? <!-- brief:content -->

_Open._

## Services

### Which mail provider in production? <!-- brief:mail -->

_Open._

### Which language model provider? <!-- brief:model -->

_Open._

### How much may the model cost a month? <!-- brief:model_budget -->

_Open._

### Where do files go in production? <!-- brief:storage -->

_Open._

### Payments? <!-- brief:payments -->

_Open._

## Deployment

### Where will it run? <!-- brief:hosting -->

_Open._

### Which domain? <!-- brief:domain -->

_Open._

### How many people in the first year? <!-- brief:scale -->

_Open._

### Where must the data stay? <!-- brief:region -->

_Open._

## Working agreements

### When should an agent push? <!-- brief:push -->

Commit and push completed modules to agim/zing main after verification. Regular tests run at feature/module completion and full release boundaries. Keep repository public; deployment instance privacy is configured independently.

### What must an agent ask before doing? <!-- brief:approval -->

_Open._

### What is the test bar? <!-- brief:tests -->

_Open._

### What else does done mean? <!-- brief:done -->

_Open._

### How should an agent report? <!-- brief:reports -->

_Open._

## Notes

Anything else the team should know that no question above asks.
