#!/usr/bin/python3

import smtplib, os, sys

port = 465
smtp_server = os.environ.get("SMTP_SERVER")
sender_email = os.environ.get("SENDER_EMAIL")
receiver_email = os.environ.get("RECIEVER_EMAIL")
password = os.environ.get("SENDER_EMAIL_PASSWORD")
message = sys.stdin.read()

with smtplib.SMTP_SSL(smtp_server, port) as server:
    server.login(sender_email, password)
    server.sendmail(sender_email, receiver_email, message)
    server.quit()