vcl 4.1;

backend default {
    .host = "127.0.0.1";
    .port = "6082";
    .connect_timeout = 10s;
    .first_byte_timeout = 300s;
    .between_bytes_timeout = 300s;
}

acl purge {
    "localhost";
    "127.0.0.1";
}

sub vcl_recv {
    if (req.restarts == 0) {
        if (req.http.X-Forwarded-For) {
            set req.http.X-Forwarded-For = req.http.X-Forwarded-For + ", " + client.ip;
        } else {
            set req.http.X-Forwarded-For = client.ip;
        }
    }

    if (req.method == "PURGE") {
        if (!client.ip ~ purge) {
            return (synth(405, "Not allowed."));
        }
        return (purge);
    }

    if (req.method != "GET" && req.method != "HEAD") {
        set req.http.X-Cache-Bypass = "method";
        return (pass);
    }

    if (req.http.Authorization) {
        set req.http.X-Cache-Bypass = "authorization";
        return (pass);
    }

    if (req.http.Cookie) {
        set req.http.X-Cache-Bypass = "cookie";
        return (pass);
    }

    if (req.http.Cache-Control ~ "(?i)no-cache|no-store|max-age=0") {
        unset req.http.Cache-Control;
    }
    if (req.http.Pragma ~ "(?i)no-cache") {
        unset req.http.Pragma;
    }
}

sub vcl_backend_response {
    if (beresp.http.Cache-Control ~ "(?i)no-cache|no-store|private") {
        set beresp.http.X-Cache-Status = "BYPASS";
        set beresp.http.X-Cache-Reason = "cache-control";
        set beresp.uncacheable = true;
        return (deliver);
    }

    if (beresp.http.Vary == "*") {
        set beresp.http.X-Cache-Status = "BYPASS";
        set beresp.http.X-Cache-Reason = "vary-star";
        set beresp.uncacheable = true;
        return (deliver);
    }

    if (beresp.http.Set-Cookie) {
        set beresp.http.X-Cache-Status = "BYPASS";
        set beresp.http.X-Cache-Reason = "set-cookie";
        set beresp.uncacheable = true;
        return (deliver);
    }

    if (beresp.ttl <= 0s) {
        set beresp.http.X-Cache-Status = "DYNAMIC";
        set beresp.http.X-Cache-Reason = "ttl-zero";
        set beresp.uncacheable = true;
        return (deliver);
    }

    set beresp.http.X-Cache-Status = "MISS";
    set beresp.http.X-Cache-Reason = "cacheable";

    if (beresp.http.Content-Length ~ "[0-9]{8,}") {
        set beresp.http.X-Cache-Status = "BYPASS";
        set beresp.http.X-Cache-Reason = "content-length-large";
        set beresp.uncacheable = true;
        return (deliver);
    }
}

sub vcl_deliver {
    if (obj.hits > 0) {
        set resp.http.X-Varnish-Cache = "HIT";
    } else if (req.http.X-Cache-Bypass) {
        set resp.http.X-Varnish-Cache = "BYPASS";
        set resp.http.X-Varnish-Reason = req.http.X-Cache-Bypass;
    } else if (resp.http.X-Cache-Status) {
        set resp.http.X-Varnish-Cache = resp.http.X-Cache-Status;
    } else {
        set resp.http.X-Varnish-Cache = "MISS";
    }

    if (resp.http.X-Cache-Reason) {
        set resp.http.X-Varnish-Reason = resp.http.X-Cache-Reason;
    }
    set resp.http.X-Cache-Status = resp.http.X-Varnish-Cache;
    unset resp.http.X-Cache-Status;
    unset resp.http.X-Cache-Reason;
    unset req.http.X-Cache-Bypass;
}
