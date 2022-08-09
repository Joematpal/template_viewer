# Template Viewer

## Run Local
```
go run main.go
```

## Viewing
While running "go run main.go":  
Open preferred browser.  
Visit localhost:8080  

## Usage
In the "Template File Path", place the directory and file name with file extension in this box for where the template resides on your drive in the format:
"<drive letter>/<repo directory>/sai-go-accounts/email/templates/<language>/<template name>.html"

In the "Loadable Data" field, use a JSON payload format to define necessary variables with garbage data so that they are visible in the template. For instance, on line 133 of the "email_forgotpassword.html" template, we find the variables needed: "{{ range $Username, $URL := .Accounts }}"

So in the "Loadable Data" field, we place garbage data for a Username and URL, as needed:
```
{
    "Accounts": {
        "testdata@test.com": "www.google.com"
    }
}
```

## Tips
Use the "Inspect" option to see resources failing to load in the template.

## Running - start --help

```
NAME:
   template-viewer start

USAGE:
   template-viewer start [command options] [arguments...]

OPTIONS:
   --engine value  (default: empty or `liquid`; default go template or liquid)
   --host value    (default: "0.0.0.0")
   --port value    (default: "8080")
```
