import { useEffect, useState } from 'react'
import App from './App'
import DocumentationSite from './DocumentationSite'
import { documentationTopicFromHash } from './documentation'

export default function Root() {
  const [documentationTopic, setDocumentationTopic] = useState(() => documentationTopicFromHash(window.location.hash))

  useEffect(() => {
    function syncRoute() {
      setDocumentationTopic(documentationTopicFromHash(window.location.hash))
    }
    window.addEventListener('hashchange', syncRoute)
    return () => window.removeEventListener('hashchange', syncRoute)
  }, [])

  useEffect(() => {
    if (!documentationTopic) document.title = 'StewardMesh'
  }, [documentationTopic])

  return documentationTopic ? <DocumentationSite topicID={documentationTopic} /> : <App />
}
