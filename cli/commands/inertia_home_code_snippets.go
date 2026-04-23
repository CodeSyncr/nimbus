package commands

// Highlighted HTML for <pre> blocks on Inertia home pages (embedded as JS template literals).
const inertiaHomeHeroCodeHTML = `<span class="kw">package</span> controllers

<span class="kw">import</span> (
  <span class="str">"github.com/CodeSyncr/nimbus/http"</span>
  <span class="str">"your-app/app/models"</span>
)

<span class="cmt">// Index returns a paginated list of users</span>
<span class="kw">func</span> (<span class="type">UsersController</span>) <span class="fn">Index</span>(
  ctx <span class="op">*</span><span class="type">http.Context</span>,
) <span class="kw">error</span> {

  users, err <span class="op">:=</span> models.<span class="type">User</span>.<span class="fn">Query</span>().
    <span class="fn">Where</span>(<span class="str">"active"</span>, <span class="num">true</span>).
    <span class="fn">OrderBy</span>(<span class="str">"created_at"</span>, <span class="str">"desc"</span>).
    <span class="fn">Paginate</span>(ctx.<span class="fn">QueryInt</span>(<span class="str">"page"</span>, <span class="num">1</span>), <span class="num">20</span>)

  <span class="kw">if</span> err <span class="op">!=</span> <span class="num">nil</span> {
    <span class="kw">return</span> ctx.<span class="fn">InternalServerError</span>(err)
  }

  <span class="kw">return</span> ctx.<span class="fn">View</span>(<span class="str">"users/index"</span>, <span class="type">ViewData</span>{
    <span class="str">"users"</span>: users,
    <span class="str">"meta"</span>:  users.<span class="fn">Meta</span>(),
  })
}`

const inertiaHomeBottomCodeHTML = `<span class="kw">package</span> controllers

<span class="kw">import</span> (
  <span class="str">"github.com/CodeSyncr/nimbus/http"</span>
  <span class="str">"your-app/app/models"</span>
)

<span class="cmt">// Index returns a paginated list of users</span>
<span class="kw">func</span> (<span class="type">UsersController</span>) <span class="fn">Index</span>(ctx <span class="op">*</span><span class="type">http.Context</span>) <span class="kw">error</span> {
  users, err <span class="op">:=</span> models.<span class="type">User</span>.<span class="fn">Query</span>().
    <span class="fn">Where</span>(<span class="str">"active"</span>, <span class="num">true</span>).
    <span class="fn">OrderBy</span>(<span class="str">"created_at"</span>, <span class="str">"desc"</span>).
    <span class="fn">Paginate</span>(ctx.<span class="fn">QueryInt</span>(<span class="str">"page"</span>, <span class="num">1</span>), <span class="num">20</span>)

  <span class="kw">if</span> err <span class="op">!=</span> <span class="num">nil</span> {
    <span class="kw">return</span> ctx.<span class="fn">InternalServerError</span>(err)
  }

  <span class="kw">return</span> ctx.<span class="fn">View</span>(<span class="str">"users/index"</span>, <span class="type">ViewData</span>{
    <span class="str">"users"</span>: users,
    <span class="str">"meta"</span>:  users.<span class="fn">Meta</span>(),
  })
}`
