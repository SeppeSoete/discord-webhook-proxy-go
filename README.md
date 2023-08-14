# Discord-webhook-proxy-go

## About

This project provides a proxy on top of the discord webhooks api.
The server provides a few authentication endpoints for managing access to the webhook.
It is designed to work inside of the google cloud platform, specifically google
appengine and firestore.

## Usage

The app includes a basic admin panel, accessible via the /admin endpoint
It allows a user with a valid admin token to perform the following actions:

- Create a token for a new user
- Delete all tokens for a user
- Promote a user to admin
- Find all tokens for a user

## Deploying the app

- Create a google appengine project
- Add firebase to the project
- Create an app.yaml file to configure scaling and the runtime.
This app.yaml file should also declare all the environment variables
as specified in env.skel
- Use the google cloud tools to deploy the project
