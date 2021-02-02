#!/usr/bin/python

import smtplib, os, sys

port = 587
smtp_server = os.environ.get("SMTP_SERVER")
sender_email = os.environ.get("SENDER_EMAIL")
receiver_email = os.environ.get("RECIEVER_EMAIL")
password = os.environ.get("SENDER_EMAIL_PASSWORD")
message = sys.stdin.read()

with smtplib.SMTP(smtp_server, port) as server:
    server.starttls()
    server.login(sender_email, password)
    server.sendmail(sender_email, receiver_email, message)